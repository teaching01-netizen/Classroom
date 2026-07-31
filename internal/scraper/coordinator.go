package scraper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"qr-command-center/internal/db"
	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
	"qr-command-center/internal/warwick"
)

type Source interface {
	Fetch(context.Context, domain.ScrapeTarget) (warwick.SnapshotFetchResult, error)
}

// ConfirmationSource is implemented by sources that can perform an
// independent, unconditional refetch of a target. It is used to confirm
// suspicious candidates before a destructive change may be published.
type ConfirmationSource interface {
	FetchConfirmation(context.Context, domain.ScrapeTarget) (warwick.SnapshotFetchResult, error)
}

// Version identity recorded on every published snapshot and candidate.
const (
	ParserVersion       = "warwick-v1"
	SchemaVersion       = "schema-v1"
	CanonicalizerVersion = "canonical-v1"
)

type Store interface {
	Commit(context.Context, db.CommitInput) (db.CommitResult, error)
	ReleaseLease(context.Context, db.ReleaseLeaseRequest) error
	RenewLease(context.Context, db.RenewLeaseRequest) error
	ReconcileLifecycle(context.Context, db.LifecycleReconcileInput) error
}

type HostObserver interface {
	Observe(context.Context, domain.HostObservation) error
}

type CoordinatorConfig struct {
	FetchTimeout          time.Duration
	CanonicalPayloadLimit int64
	Clock                 func() time.Time
	Random                *mathrand.Rand
}

type Coordinator struct {
	source                Source
	store                 Store
	observer              HostObserver
	fetchTimeout          time.Duration
	canonicalPayloadLimit int64
	clock                 func() time.Time
	randomMu              sync.Mutex
	random                *mathrand.Rand
}

func NewCoordinator(
	source Source,
	store Store,
	observer HostObserver,
	config CoordinatorConfig,
) *Coordinator {
	if source == nil {
		panic("Coordinator: source must not be nil")
	}
	if store == nil {
		panic("Coordinator: store must not be nil")
	}
	if observer == nil {
		panic("Coordinator: observer must not be nil")
	}
	if config.FetchTimeout <= 0 {
		panic("Coordinator: fetch timeout must be positive")
	}
	if config.CanonicalPayloadLimit <= 0 {
		panic("Coordinator: canonical payload limit must be positive")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	}
	return &Coordinator{
		source:                source,
		store:                 store,
		observer:              observer,
		fetchTimeout:          config.FetchTimeout,
		canonicalPayloadLimit: config.CanonicalPayloadLimit,
		clock:                 config.Clock,
		random:                config.Random,
	}
}

type RunResult struct {
	TargetID        int64
	LeaseGeneration int64
	Outcome         string
	Succeeded       bool
	Changed         bool
	NextRunAt       time.Time
	Commit          db.CommitResult
}

func (c *Coordinator) RunClaimed(
	ctx context.Context,
	target domain.ScrapeTarget,
) (RunResult, error) {
	return c.RunClaimedWithRelease(ctx, target, nil)
}

func (c *Coordinator) RunClaimedWithRelease(
	ctx context.Context,
	target domain.ScrapeTarget,
	releaseAfterFetch func(),
) (RunResult, error) {
	if target.ID <= 0 || target.LeaseGeneration <= 0 {
		return RunResult{}, domain.ErrLeaseLost
	}
	if err := target.Ref.Validate(); err != nil {
		return RunResult{}, err
	}
	startedAt := c.clock().UTC()
	fetchCtx, cancel := context.WithTimeout(ctx, c.fetchTimeout)
	fetchResult, fetchErr := c.source.Fetch(fetchCtx, target)
	cancel()
	if releaseAfterFetch != nil {
		releaseAfterFetch()
	}
	finishedAt := c.clock().UTC()

	if canceledFetch(ctx, fetchErr) {
		releaseErr := c.store.ReleaseLease(ctx, db.ReleaseLeaseRequest{
			TargetID:        target.ID,
			LeaseGeneration: target.LeaseGeneration,
		})
		if releaseErr != nil {
			return RunResult{}, errors.Join(fetchErr, releaseErr)
		}
		if fetchErr == nil {
			fetchErr = context.Canceled
		}
		return RunResult{}, fetchErr
	}

	outcome, errorKind, statusCode, retryAfter := classifyFetch(fetchResult, fetchErr)
	var payload json.RawMessage
	var contentHash [32]byte
	changed := false
	var recordsCount int
	var validationWarnings []ValidationWarning
	var manifest domain.SnapshotManifest
	var validationReport domain.ValidationReport
	confirmationGroup := ""
	confirmationRejectionCode := ""
	var candidates []domain.ScrapeCandidate
	if fetchErr == nil {
		var canonicalErr error
		payload, contentHash, recordsCount, canonicalErr = Canonicalize(
			target.Ref.Kind,
			fetchResult.Value,
			c.canonicalPayloadLimit,
		)
		if canonicalErr != nil {
			fetchErr = canonicalErr
			outcome = "invalid_payload"
			errorKind = "canonicalization"
			metrics.ScrapeValidationFailedTotal.Inc()
		} else {
			validated, validationErr := ValidatePayload(target.Ref.Kind, fetchResult.Value)
			if validationErr != nil {
				fetchErr = validationErr
				outcome = "invalid_payload"
				errorKind = "validation"
				payload = nil
				contentHash = [32]byte{}
				recordsCount = 0
				metrics.ScrapeValidationFailedTotal.Inc()
			} else {
				validationWarnings = validated.Warnings
				previousCount := 0
				if target.HasCurrentSnapshot {
					previousCount = target.PreviousRecordCount
				}
				classification := ClassifyChange(DefaultChangeSafetyPolicy(), validated, previousCount)
				manifest = buildSnapshotManifest(fetchResult, validated)
				validationReport = buildValidationReport(
					manifest,
					validated.Warnings,
					classification.Status == "suspicious",
				)

				if classification.Status == "suspicious" {
					confirmationGroup = newConfirmationGroup()
					confirmed, confirmationCode, confirmationEvidence := c.confirmChange(
						ctx,
						target,
						contentHash,
						recordsCount,
					)
					if !confirmed {
						outcome = "quarantined"
						errorKind = "confirmation"
						fetchErr = fmt.Errorf(
							"suspicious change was not independently confirmed: %s",
							confirmationCode,
						)
						changed = false
						confirmationRejectionCode = confirmationCode
						if confirmationEvidence != nil {
							confirmationEvidence.ConfirmationGroupUUID = confirmationGroup
							candidates = append(candidates, *confirmationEvidence)
						}
					}
				}

				if outcome != "quarantined" {
					changed = !target.HasContentHash() || contentHash != target.CurrentContentHash
					if changed {
						outcome = "changed"
					} else {
						outcome = "unchanged"
						payload = nil
						contentHash = [32]byte{}
					}
				}
			}
		}
	} else if errors.Is(fetchErr, domain.ErrNotModified) {
		if target.HasCurrentSnapshot {
			outcome = "not_modified"
			fetchErr = nil
		} else {
			outcome = "invalid_payload"
			errorKind = "not_modified_without_snapshot"
			fetchErr = errors.New("upstream returned not modified without a current snapshot")
		}
	}

	successful := isSuccessfulOutcome(outcome)
	lastRejectionCode := ""
	if !successful && errorKind != "" {
		lastRejectionCode = errorKind
	}
	if confirmationRejectionCode != "" {
		lastRejectionCode = confirmationRejectionCode
	}
	switch outcome {
	case "changed", "unchanged", "quarantined":
		disposition := domain.CandidateAccepted
		rejectionCode := ""
		switch outcome {
		case "unchanged":
			disposition = domain.CandidateUnchanged
		case "quarantined":
			disposition = domain.CandidateQuarantinedAnomaly
			rejectionCode = lastRejectionCode
		}
		candidates = append(candidates, domain.ScrapeCandidate{
			TargetID:              target.ID,
			LeaseGeneration:       target.LeaseGeneration,
			AttemptNumber:         1,
			FetchedAt:             finishedAt,
			RequestID:             newRequestID(),
			HTTPStatus:            statusCodeValue(statusCode),
			ContentType:           fetchResult.Metadata.ContentType,
			ContentLength:         fetchResult.BytesRead,
			ETag:                  fetchResult.Metadata.ETag,
			LastModified:          fetchResult.Metadata.LastModified,
			RawBodyHash:           fetchResult.Metadata.RawBodyHash,
			CanonicalHash:         hex.EncodeToString(contentHash[:]),
			ParserVersion:         ParserVersion,
			SchemaVersion:         SchemaVersion,
			CanonicalizerVersion:  CanonicalizerVersion,
			Payload:               payload,
			Manifest:              manifest,
			Validation:            validationReport,
			Disposition:           disposition,
			RejectionCode:         rejectionCode,
			ConfirmationGroupUUID: confirmationGroup,
		})
	}
	if outcome == "quarantined" {
		// Candidate rows carry the full evidence; the run commit itself
		// must not reference payload or content hash so nothing is published
		// and the last-known-good snapshot remains authoritative.
		payload = nil
		contentHash = [32]byte{}
		manifest = domain.SnapshotManifest{}
		validationReport = domain.ValidationReport{}
	}
	currentInterval := target.CurrentInterval
	recentChanges := append([]bool(nil), target.RecentChanges...)
	consecutiveFailures := target.ConsecutiveFailures
	var validatedAt *time.Time
	var validationSeqAfter *int64
	var nextRunAt time.Time
	if successful {
		policy := policyForTarget(target)
		scheduleOutcome := OutcomeUnchanged
		switch outcome {
		case "changed":
			scheduleOutcome = OutcomeChanged
		case "not_modified":
			scheduleOutcome = OutcomeNotModified
		}
		schedule := NextSchedule(policy, currentInterval, recentChanges, scheduleOutcome)
		currentInterval = schedule.Interval
		recentChanges = schedule.History
		consecutiveFailures = 0
		value := finishedAt
		validatedAt = &value
		seq := target.ValidationSeq + 1
		validationSeqAfter = &seq
		c.randomMu.Lock()
		nextRunAt = finishedAt.Add(ApplyJitter(currentInterval, 0.1, c.random))
		c.randomMu.Unlock()
	} else {
		consecutiveFailures++
		c.randomMu.Lock()
		nextRunAt = failureNextRun(
			finishedAt,
			outcome,
			consecutiveFailures,
			retryAfter,
			c.random,
		)
		c.randomMu.Unlock()
	}

	observationErr := c.observer.Observe(ctx, domain.HostObservation{
		Host:       target.Ref.Host,
		Outcome:    outcome,
		StatusCode: statusCodeValue(statusCode),
		RetryAfter: retryAfter,
		Latency:    finishedAt.Sub(startedAt),
		ConsecutiveFailures: func() int {
			if successful {
				return 0
			}
			return consecutiveFailures
		}(),
		ObservedAt: finishedAt,
	})

	var discovered []domain.TargetSeed
	var seen []domain.TargetRef
	if outcome == "changed" || outcome == "unchanged" {
		discovered, seen = discoverTargets(target, fetchResult.Value, finishedAt)
		// Add jitter to next_run_at for newly discovered children to avoid a
		// scrape storm when a parent reveals many children at once.
		c.randomMu.Lock()
		for i := range discovered {
			jitter := time.Duration(c.random.Int63n(int64(time.Minute * 5)))
			discovered[i].NextRunAt = discovered[i].NextRunAt.Add(jitter)
		}
		c.randomMu.Unlock()
	}
	input := db.CommitInput{
		TargetID:            target.ID,
		WorkerID:            target.LeaseOwner,
		LeaseGeneration:     target.LeaseGeneration,
		Outcome:             outcome,
		StartedAt:           startedAt,
		FinishedAt:          finishedAt,
		HTTPStatus:          statusCode,
		BytesRead:           fetchResult.BytesRead,
		ErrorKind:           errorKind,
		ErrorMessage:        safeUpstreamError(fetchErr),
		NextRunAt:           nextRunAt,
		CurrentInterval:     currentInterval,
		ConsecutiveFailures: consecutiveFailures,
		RecentChanges:       recentChanges,
		ValidatedAt:         validatedAt,
		ValidationSeqAfter:  validationSeqAfter,
		ETag:                fetchResult.Metadata.ETag,
		LastModified:        fetchResult.Metadata.LastModified,
		CacheControl:        fetchResult.Metadata.CacheControl,
		Changed:             changed,
		ContentHash:         contentHash,
		Payload:             payload,
		Discovered:          discovered,
		SeenChildRefs:       seen,
		RecordsCount:        recordsCount,
		Manifest:            manifest,
		Validation:          validationReport,
		ParserVersion:       ParserVersion,
		SchemaVersion:       SchemaVersion,
		RawBodyHash:         fetchResult.Metadata.RawBodyHash,
		LastRejectionCode:   lastRejectionCode,
		Candidates:          candidates,
	}
	commitResult, commitErr := c.store.Commit(ctx, input)

	// Structured logging for scrape execution observability
	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	contentHashStr := ""
	if changed {
		contentHashStr = hex.EncodeToString(contentHash[:])
	}
	previousContentHashStr := ""
	if target.HasContentHash() {
		previousContentHashStr = hex.EncodeToString(target.CurrentContentHash[:])
	}

	// Determine validation and commit statuses
	validationStatus := "ok"
	if outcome == "invalid_payload" {
		validationStatus = "failed"
	}
	commitStatus := "ok"
	if commitErr != nil {
		commitStatus = "failed"
	}

	// Build structured log fields
	logFields := []any{
		"target_id", target.ID,
		"target_kind", string(target.Ref.Kind),
		"course_id", target.Ref.ParentKey,
		"session_id", target.Ref.ResourceKey,
		"worker_id", target.LeaseOwner,
		"lease_generation", target.LeaseGeneration,
		"previous_snapshot_version", target.CurrentVersion,
		"previous_content_hash", previousContentHashStr,
		"new_content_hash", contentHashStr,
		"canonicalization_version", 1,
		"fetch_status", outcome,
		"validation_status", validationStatus,
		"commit_status", commitStatus,
		"duration_ms", durationMs,
		"response_bytes", fetchResult.BytesRead,
		"records_count", recordsCount,
		"validation_warnings", len(validationWarnings),
	}
	if commitErr == nil {
		logFields = append(logFields,
			"run_id", commitResult.RunID,
			"new_snapshot_version", func() int64 {
				if commitResult.Snapshot != nil {
					return commitResult.Snapshot.Version
				}
				return 0
			}(),
		)
	}

	// Log based on outcome
	switch {
	case !successful:
		slog.Error("scrape_execution_failed", append(logFields,
			"error_kind", errorKind,
			"error_message", safeUpstreamError(fetchErr),
		)...)
	case outcome == "rate_limited":
		slog.Warn("scrape_execution_rate_limited", append(logFields,
			"retry_after", retryAfter.Seconds(),
		)...)
	default:
		slog.Info("scrape_execution_completed", logFields...)
	}

	if commitErr != nil {
		return RunResult{}, errors.Join(commitErr, observationErr)
	}

	// Reconcile child target lifecycle states after every successful parent
	// fetch. This marks absent children as missing, tombstones them after
	// the configured threshold, and reactivates any previously tombstoned
	// children that reappear.
	if successful {
		parentVersion := target.CurrentVersion
		if commitResult.Snapshot != nil {
			parentVersion = commitResult.Snapshot.Version
		}
		if reconcileErr := c.store.ReconcileLifecycle(ctx, db.LifecycleReconcileInput{
			ParentRef:       target.Ref,
			ParentVersion:   parentVersion,
			DiscoveredSeeds: discovered,
			SeenChildRefs:   seen,
		}); reconcileErr != nil {
			slog.Error("lifecycle_reconciliation_failed",
				"target_id", target.ID,
				"target_kind", string(target.Ref.Kind),
				"error", reconcileErr,
			)
		}
	}

	result := RunResult{
		TargetID:        target.ID,
		LeaseGeneration: target.LeaseGeneration,
		Outcome:         outcome,
		Succeeded:       successful,
		Changed:         changed,
		NextRunAt:       nextRunAt,
		Commit:          commitResult,
	}
	kind := string(target.Ref.Kind)
	metrics.WarwickScrapeRunsTotal.WithLabelValues(kind, outcome).Inc()
	metrics.WarwickScrapeDurationSeconds.
		WithLabelValues(kind, outcome).
		Observe(finishedAt.Sub(startedAt).Seconds())
	if observationErr != nil {
		return result, observationErr
	}
	return result, nil
}

func canceledFetch(parent context.Context, err error) bool {
	if parent.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled)
}

func classifyFetch(
	result warwick.SnapshotFetchResult,
	err error,
) (string, string, *int, time.Duration) {
	var statusCode *int
	if result.Metadata.StatusCode > 0 {
		value := result.Metadata.StatusCode
		statusCode = &value
	}
	if err == nil {
		return "unchanged", "", statusCode, 0
	}
	if errors.Is(err, domain.ErrNotModified) {
		return "not_modified", "", statusCode, 0
	}
	var statusErr *domain.UpstreamStatusError
	if errors.As(err, &statusErr) {
		value := statusErr.StatusCode
		statusCode = &value
		switch statusErr.StatusCode {
		case http.StatusTooManyRequests:
			return "rate_limited", "rate_limited", statusCode, statusErr.RetryAfter
		case http.StatusRequestTimeout, http.StatusTooEarly,
			http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return "transient_error", "upstream_status", statusCode, statusErr.RetryAfter
		case http.StatusNotFound, http.StatusGone:
			return "not_found", "not_found", statusCode, 0
		default:
			if statusErr.StatusCode >= 400 && statusErr.StatusCode < 500 {
				return "permanent_error", "upstream_status", statusCode, 0
			}
			return "transient_error", "upstream_status", statusCode, 0
		}
	}
	if errors.Is(err, domain.ErrAuthExpired) || errors.Is(err, domain.ErrAuthConflict) {
		return "auth_error", "authentication", statusCode, 0
	}
	var fetchErr *domain.FetchError
	if errors.As(err, &fetchErr) {
		switch fetchErr.Kind {
		case domain.ErrKindInvalidPayload:
			return "invalid_payload", "invalid_payload", statusCode, 0
		case domain.ErrKindRateLimited:
			return "rate_limited", "rate_limited", statusCode, 0
		case domain.ErrKindAuthExpired, domain.ErrKindAuthConflict:
			return "auth_error", "authentication", statusCode, 0
		default:
			return "transient_error", "network", statusCode, 0
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "transient_error", "timeout", statusCode, 0
	}
	return "transient_error", "network", statusCode, 0
}

func statusCodeValue(status *int) int {
	if status == nil {
		return 0
	}
	return *status
}

func isSuccessfulOutcome(outcome string) bool {
	return outcome == "changed" || outcome == "unchanged" || outcome == "not_modified"
}

func policyForTarget(target domain.ScrapeTarget) Policy {
	var attributes struct {
		SessionStatus domain.SessionStatus `json:"session_status"`
	}
	_ = json.Unmarshal(target.Attributes, &attributes)
	policy := PolicyFor(target.Ref.Kind, attributes.SessionStatus)
	if target.MinInterval > 0 {
		policy.Min = target.MinInterval
	}
	if target.MaxInterval > 0 {
		policy.Max = target.MaxInterval
	}
	if target.MaxServeAge > 0 {
		policy.MaxServeAge = target.MaxServeAge
	}
	if target.CurrentInterval > 0 {
		policy.Initial = target.CurrentInterval
	}
	return policy
}

func failureNextRun(
	now time.Time,
	outcome string,
	consecutiveFailures int,
	retryAfter time.Duration,
	rng *mathrand.Rand,
) time.Time {
	switch outcome {
	case "rate_limited":
		pause := retryAfter
		if pause < 15*time.Minute {
			pause = 15 * time.Minute
		}
		return now.Add(pause)
	case "auth_error":
		return now.Add(15 * time.Minute)
	case "not_found":
		return now.Add(24 * time.Hour)
	case "permanent_error":
		return now.Add(24 * time.Hour)
	case "invalid_payload":
		return now.Add(time.Hour)
	case "quarantined":
		return now.Add(time.Hour)
	default:
		delay := FullJitter(FailureDelay(consecutiveFailures), rng)
		if retryAfter > delay {
			delay = retryAfter
		}
		return now.Add(delay)
	}
}

func discoverTargets(
	parent domain.ScrapeTarget,
	value any,
	now time.Time,
) ([]domain.TargetSeed, []domain.TargetRef) {
	var seeds []domain.TargetSeed
	switch parent.Ref.Kind {
	case domain.SnapshotCourseCatalog:
		courses, ok := value.([]domain.CourseSummary)
		if !ok {
			return nil, nil
		}
		for _, course := range courses {
			policy := PolicyFor(domain.SnapshotCourseDetail, "")
			attributes, _ := json.Marshal(map[string]any{
				"course_name":   course.Name,
				"course_status": course.Status,
			})
			seeds = append(seeds, domain.TargetSeed{
				Ref: domain.TargetRef{
					Host: parent.Ref.Host, Kind: domain.SnapshotCourseDetail,
					ResourceKey: course.CourseID,
				},
				Attributes:      attributes,
				Priority:        domain.PriorityActiveCourse,
				InitialInterval: policy.Initial,
				MinInterval:     policy.Min,
				MaxInterval:     policy.Max,
				MaxServeAge:     policy.MaxServeAge,
				NextRunAt:       now,
			})
		}
	case domain.SnapshotCourseDetail:
		detail, ok := value.(*domain.CourseDetail)
		if !ok || detail == nil {
			if copied, valueOK := value.(domain.CourseDetail); valueOK {
				detail = &copied
			} else {
				return nil, nil
			}
		}
		for _, session := range detail.Sessions {
			policy := PolicyFor(domain.SnapshotSessionDetail, session.Status)
			attributes, _ := json.Marshal(map[string]any{
				"session_status": session.Status,
			})
			seeds = append(seeds, domain.TargetSeed{
				Ref: domain.TargetRef{
					Host: parent.Ref.Host, Kind: domain.SnapshotSessionDetail,
					ParentKey: parent.Ref.ResourceKey, ResourceKey: session.SessionID,
				},
				Attributes:      attributes,
				Priority:        domain.DefaultPriority(domain.SnapshotSessionDetail, session.Status),
				InitialInterval: policy.Initial,
				MinInterval:     policy.Min,
				MaxInterval:     policy.Max,
				MaxServeAge:     policy.MaxServeAge,
				NextRunAt:       now,
			})
		}
	}
	sort.Slice(seeds, func(i, j int) bool {
		return seeds[i].Ref.IdentityKey() < seeds[j].Ref.IdentityKey()
	})

	seen := make([]domain.TargetRef, len(seeds))
	for index := range seeds {
		seen[index] = seeds[index].Ref
	}
	return seeds, seen
}

var upstreamSecretAssignmentPattern = regexp.MustCompile(
	`(?i)\b(asp\.net_sessionid|cookie|authorization|password|credential|token)\b\s*[:=]\s*[^,;\r\n]+`,
)

var upstreamBearerSecretPattern = regexp.MustCompile(
	`(?i)\bbearer\s+[a-z0-9._~+/=-]+`,
)

func safeUpstreamError(err error) string {
	if err == nil {
		return ""
	}
	message := upstreamSecretAssignmentPattern.ReplaceAllString(err.Error(), "$1=<redacted>")
	message = upstreamBearerSecretPattern.ReplaceAllString(message, "Bearer <redacted>")
	if len(message) > 2048 {
		message = message[:2048]
	}
	return strings.ToValidUTF8(message, "")
}

// confirmChange re-fetches a suspicious candidate without conditional headers
// and accepts it only when the independent fetch canonicalizes to the same
// hash and record count. On a mismatch it returns the confirmation-fetch
// evidence so the caller can persist both candidates under one group.
func (c *Coordinator) confirmChange(
	ctx context.Context,
	target domain.ScrapeTarget,
	firstHash [32]byte,
	firstCount int,
) (bool, string, *domain.ScrapeCandidate) {
	source, ok := c.source.(ConfirmationSource)
	if !ok {
		return false, "confirmation_unavailable", nil
	}
	fetchCtx, cancel := context.WithTimeout(ctx, c.fetchTimeout)
	defer cancel()
	second, err := source.FetchConfirmation(fetchCtx, target)
	if err != nil {
		return false, "confirmation_fetch_failed", nil
	}
	secondPayload, secondHash, secondCount, err := Canonicalize(
		target.Ref.Kind,
		second.Value,
		c.canonicalPayloadLimit,
	)
	if err != nil {
		return false, "confirmation_parse_failed", nil
	}
	if secondHash == firstHash && secondCount == firstCount {
		return true, "", nil
	}
	evidence := &domain.ScrapeCandidate{
		TargetID:             target.ID,
		LeaseGeneration:      target.LeaseGeneration,
		AttemptNumber:        2,
		FetchedAt:            c.clock().UTC(),
		RequestID:            newRequestID(),
		HTTPStatus:           second.Metadata.StatusCode,
		ContentType:          second.Metadata.ContentType,
		ContentLength:        second.BytesRead,
		ETag:                 second.Metadata.ETag,
		LastModified:         second.Metadata.LastModified,
		RawBodyHash:          second.Metadata.RawBodyHash,
		CanonicalHash:        hex.EncodeToString(secondHash[:]),
		ParserVersion:        ParserVersion,
		SchemaVersion:        SchemaVersion,
		CanonicalizerVersion: CanonicalizerVersion,
		Payload:              secondPayload,
		Manifest:             manifestFromEvidence(second, secondCount),
		Validation: domain.ValidationReport{
			Complete: secondCount == second.ReportedCount ||
				second.ReportedCount <= 0,
			Violations: []domain.ValidationViolation{{
				Code:     "confirmation_mismatch",
				Severity: domain.SeverityFatal,
				Message: "independent confirmation fetch did not match " +
					"the original candidate",
			}},
		},
		Disposition:   domain.CandidateQuarantinedAnomaly,
		RejectionCode: "confirmation_mismatch",
	}
	return false, "confirmation_mismatch", evidence
}

// buildSnapshotManifest records pagination and count evidence so a snapshot
// can prove it is complete. Fetch functions reject incomplete collections
// before returning, so a built manifest is complete unless counts disagree.
func buildSnapshotManifest(
	result warwick.SnapshotFetchResult,
	validated *ValidatedPayload,
) domain.SnapshotManifest {
	manifest := domain.SnapshotManifest{
		SourceReportedCount: result.ReportedCount,
		ParsedCount:         validated.RecordCount,
		UniqueCount:         validated.DistinctIDs,
		ExpectedPageCount:   result.ExpectedPages,
		FetchedPageCount:    result.FetchedPages,
	}
	manifest.Complete =
		(manifest.ExpectedPageCount <= 0 ||
			manifest.FetchedPageCount >= manifest.ExpectedPageCount) &&
			(manifest.SourceReportedCount <= 0 ||
				manifest.ParsedCount == manifest.SourceReportedCount)
	if !manifest.Complete {
		manifest.IncompleteReasons = append(manifest.IncompleteReasons, "parsed count differs from reported count")
	}
	return manifest
}

// manifestFromEvidence builds a completeness manifest for a confirmation
// fetch, which is not run through the full validation pipeline.
func manifestFromEvidence(
	result warwick.SnapshotFetchResult,
	parsedCount int,
) domain.SnapshotManifest {
	manifest := domain.SnapshotManifest{
		SourceReportedCount: result.ReportedCount,
		ParsedCount:         parsedCount,
		UniqueCount:         parsedCount,
		ExpectedPageCount:   result.ExpectedPages,
		FetchedPageCount:    result.FetchedPages,
	}
	manifest.Complete =
		(manifest.ExpectedPageCount <= 0 ||
			manifest.FetchedPageCount >= manifest.ExpectedPageCount) &&
			(manifest.SourceReportedCount <= 0 ||
				manifest.ParsedCount == manifest.SourceReportedCount)
	if !manifest.Complete {
		manifest.IncompleteReasons = append(manifest.IncompleteReasons, "parsed count differs from reported count")
	}
	return manifest
}

func buildValidationReport(
	manifest domain.SnapshotManifest,
	warnings []ValidationWarning,
	requiresConfirmation bool,
) domain.ValidationReport {
	report := domain.ValidationReport{
		Complete:             manifest.Complete,
		RequiresConfirmation: requiresConfirmation,
	}
	if !manifest.Complete {
		report.Add(
			"incomplete_manifest",
			domain.SeverityFatal,
			"",
			"candidate collection is incomplete",
		)
	}
	for _, warning := range warnings {
		report.Add(warning.Code, domain.SeverityWarning, "", warning.Message)
	}
	if requiresConfirmation {
		report.Add(
			"suspicious_change",
			domain.SeverityWarning,
			"",
			"record count change requires independent confirmation",
		)
	}
	return report
}

func newConfirmationGroup() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	)
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
