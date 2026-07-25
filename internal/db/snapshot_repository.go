package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"qr-command-center/internal/domain"
)

type SnapshotRepository struct {
	pool *pgxpool.Pool
}

func NewSnapshotRepository(pool *pgxpool.Pool) *SnapshotRepository {
	if pool == nil {
		panic("SnapshotRepository: pool must not be nil")
	}
	return &SnapshotRepository{pool: pool}
}

type HostStateSeed struct {
	Host                      string
	BaselineRequestsPerSecond float64
	Burst                     int
	BaselineConcurrency       int
	Now                       time.Time
}

type ClaimRequest struct {
	Now           time.Time
	Limit         int
	WorkerID      string
	LeaseDuration time.Duration
}

type ClaimOneRequest struct {
	Ref           domain.TargetRef
	Now           time.Time
	WorkerID      string
	LeaseDuration time.Duration
}

type ReleaseLeaseRequest struct {
	TargetID        int64
	LeaseGeneration int64
}

type CommitInput struct {
	TargetID            int64
	WorkerID            string
	LeaseGeneration     int64
	Outcome             string
	StartedAt           time.Time
	FinishedAt          time.Time
	HTTPStatus          *int
	BytesRead           int64
	ErrorKind           string
	ErrorMessage        string
	NextRunAt           time.Time
	CurrentInterval     time.Duration
	ConsecutiveFailures int
	RecentChanges       []bool
	ValidatedAt         *time.Time
	ValidationSeqAfter  *int64
	ETag                string
	LastModified        string
	CacheControl        string
	Changed             bool
	ContentHash         [32]byte
	Payload             json.RawMessage
	Discovered          []domain.TargetSeed
	SeenChildRefs       []domain.TargetRef
}

type CommitResult struct {
	RunID    int64
	Snapshot *domain.Snapshot
	Metadata *domain.SnapshotMetadata
}

type PruneRequest struct {
	Now               time.Time
	SnapshotRetention time.Duration
	RunRetention      time.Duration
	BatchSize         int
}

type PruneResult struct {
	SnapshotsDeleted int
	RunsDeleted      int
	PermitsDeleted   int
}

type ScraperStatus struct {
	Due                        int                           `json:"due"`
	DueByKind                  map[domain.SnapshotKind]int   `json:"due_by_kind"`
	Leased                     int                           `json:"leased"`
	Failed                     int                           `json:"failed"`
	ExpiredCurrent             int                           `json:"expired_current"`
	OldestValidationAgeSeconds map[domain.SnapshotKind]int64 `json:"oldest_validation_age_seconds"`
	OldestSnapshotAgeSeconds   map[domain.SnapshotKind]int64 `json:"oldest_snapshot_age_seconds"`
	HostPausedUntil            *time.Time                    `json:"host_paused_until"`
	HostRequestsPerSecond      float64                       `json:"host_requests_per_second"`
	HostConcurrency            int                           `json:"host_concurrency"`
	ActivePermits              int                           `json:"active_permits"`
	ExpiredPermits             int                           `json:"expired_permits"`
}

type AcquireHostPermitRequest struct {
	Host            string
	TargetID        int64
	WorkerID        string
	LeaseGeneration int64
	Now             time.Time
	TTL             time.Duration
}

type HostState struct {
	Host                      string
	BaselineRequestsPerSecond float64
	CurrentRequestsPerSecond  float64
	Burst                     int
	AvailableTokens           float64
	TokensUpdatedAt           time.Time
	BaselineConcurrency       int
	CurrentConcurrency        int
	Consecutive429s           int
	Last429At                 *time.Time
	HealthyStreak             int
	PausedUntil               *time.Time
}

func (r *SnapshotRepository) SeedHost(ctx context.Context, seed HostStateSeed) error {
	if strings.TrimSpace(seed.Host) == "" {
		return errors.New("seed host is required")
	}
	if seed.BaselineRequestsPerSecond < 0.25 || seed.BaselineRequestsPerSecond > 5 {
		return errors.New("seed host requests per second must be between 0.25 and 5")
	}
	if seed.Burst < 1 || seed.Burst > 5 {
		return errors.New("seed host burst must be between 1 and 5")
	}
	if seed.BaselineConcurrency < 1 || seed.BaselineConcurrency > 4 {
		return errors.New("seed host concurrency must be between 1 and 4")
	}
	if seed.Now.IsZero() {
		seed.Now = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO scrape_host_state (
			host,
			baseline_requests_per_second,
			current_requests_per_second,
			burst,
			available_tokens,
			tokens_updated_at,
			baseline_concurrency,
			current_concurrency,
			created_at,
			updated_at
		)
		VALUES (
			$1,
			$2::numeric,
			$2::numeric,
			$3::smallint,
			$3::numeric,
			$4,
			$5::smallint,
			$5::smallint,
			$4,
			$4
		)
		ON CONFLICT (host) DO UPDATE
		SET baseline_requests_per_second = EXCLUDED.baseline_requests_per_second,
			current_requests_per_second = LEAST(
				scrape_host_state.current_requests_per_second,
				EXCLUDED.baseline_requests_per_second
			),
			burst = EXCLUDED.burst,
			available_tokens = LEAST(scrape_host_state.available_tokens, EXCLUDED.burst),
			baseline_concurrency = EXCLUDED.baseline_concurrency,
			current_concurrency = LEAST(
				scrape_host_state.current_concurrency,
				EXCLUDED.baseline_concurrency
			),
			updated_at = EXCLUDED.updated_at`,
		seed.Host,
		seed.BaselineRequestsPerSecond,
		seed.Burst,
		seed.Now,
		seed.BaselineConcurrency,
	)
	if err != nil {
		return fmt.Errorf("seed scrape host: %w", err)
	}
	return nil
}

func (r *SnapshotRepository) Seed(ctx context.Context, seeds []domain.TargetSeed) error {
	if len(seeds) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("seed targets: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, seed := range seeds {
		if err := upsertSeed(ctx, tx, seed); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("seed targets: commit: %w", err)
	}
	return nil
}

type dbExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func upsertSeed(ctx context.Context, executor dbExecutor, seed domain.TargetSeed) error {
	if err := seed.Ref.Validate(); err != nil {
		return err
	}
	if seed.InitialInterval <= 0 || seed.MinInterval <= 0 ||
		seed.MaxInterval < seed.MinInterval || seed.MaxServeAge < seed.MaxInterval {
		return fmt.Errorf("invalid policy for target %s", seed.Ref.IdentityKey())
	}
	if seed.NextRunAt.IsZero() {
		return fmt.Errorf("target %s next run is required", seed.Ref.IdentityKey())
	}
	attributes := seed.Attributes
	if len(attributes) == 0 {
		attributes = json.RawMessage(`{}`)
	}
	if !json.Valid(attributes) {
		return fmt.Errorf("target %s attributes are invalid JSON", seed.Ref.IdentityKey())
	}
	_, err := executor.Exec(ctx, `
		INSERT INTO scrape_targets (
			host, kind, resource_key, parent_key, attributes,
			current_interval_seconds, min_interval_seconds,
			max_interval_seconds, max_serve_age_seconds, next_run_at
		)
		VALUES ($1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10)
		ON CONFLICT (host, kind, parent_key, resource_key) DO UPDATE
		SET attributes = EXCLUDED.attributes,
			current_interval_seconds = EXCLUDED.current_interval_seconds,
			min_interval_seconds = EXCLUDED.min_interval_seconds,
			max_interval_seconds = EXCLUDED.max_interval_seconds,
			max_serve_age_seconds = EXCLUDED.max_serve_age_seconds,
			next_run_at = LEAST(scrape_targets.next_run_at, EXCLUDED.next_run_at),
			missing_count = 0,
			enabled = TRUE,
			updated_at = NOW()`,
		seed.Ref.Host,
		seed.Ref.Kind,
		seed.Ref.ResourceKey,
		seed.Ref.ParentKey,
		attributes,
		durationSeconds(seed.InitialInterval),
		durationSeconds(seed.MinInterval),
		durationSeconds(seed.MaxInterval),
		durationSeconds(seed.MaxServeAge),
		seed.NextRunAt,
	)
	if err != nil {
		return fmt.Errorf("seed target %s: %w", seed.Ref.IdentityKey(), err)
	}
	return nil
}

func durationSeconds(value time.Duration) int64 {
	return int64(value / time.Second)
}

const targetReturningColumns = `
		target.id,
		target.host,
		target.kind,
		target.resource_key,
		target.parent_key,
		target.attributes,
		target.missing_count,
		target.current_content_hash,
		target.current_snapshot_id IS NOT NULL,
		target.current_version,
		target.validation_seq,
		target.current_interval_seconds,
		target.min_interval_seconds,
		target.max_interval_seconds,
		target.max_serve_age_seconds,
		target.next_run_at,
		target.last_validated_at,
		target.consecutive_failures,
		target.recent_changes,
		target.etag,
		target.last_modified,
		target.lease_owner,
		target.lease_generation,
		target.lease_expires_at`

func validateClaim(now time.Time, worker string, lease time.Duration) error {
	if now.IsZero() {
		return errors.New("claim time is required")
	}
	if strings.TrimSpace(worker) == "" {
		return errors.New("claim worker ID is required")
	}
	if lease <= 0 {
		return errors.New("claim lease duration must be positive")
	}
	return nil
}

func (r *SnapshotRepository) ClaimDue(ctx context.Context, request ClaimRequest) ([]domain.ScrapeTarget, error) {
	if request.Limit <= 0 {
		return nil, errors.New("claim limit must be positive")
	}
	if err := validateClaim(request.Now, request.WorkerID, request.LeaseDuration); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		WITH due AS (
			SELECT target.id
			FROM scrape_targets AS target
			JOIN scrape_host_state AS host_state ON host_state.host = target.host
			WHERE target.enabled = TRUE
			  AND target.next_run_at <= $1::timestamptz
			  AND (target.lease_expires_at IS NULL OR target.lease_expires_at <= $1::timestamptz)
			  AND (host_state.paused_until IS NULL OR host_state.paused_until <= $1::timestamptz)
			ORDER BY target.next_run_at, target.id
			FOR UPDATE OF target SKIP LOCKED
			LIMIT $2
		)
		UPDATE scrape_targets AS target
		SET lease_owner = $3,
			lease_generation = target.lease_generation + 1,
			lease_expires_at = $1::timestamptz + make_interval(secs => $4),
			last_attempt_at = $1::timestamptz,
			updated_at = $1::timestamptz
		FROM due
		WHERE target.id = due.id
		RETURNING `+targetReturningColumns,
		request.Now,
		request.Limit,
		request.WorkerID,
		durationSeconds(request.LeaseDuration),
	)
	if err != nil {
		return nil, fmt.Errorf("claim due targets: %w", err)
	}
	defer rows.Close()
	targets := make([]domain.ScrapeTarget, 0, request.Limit)
	for rows.Next() {
		target, err := scanScrapeTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim due targets: rows: %w", err)
	}
	return targets, nil
}

func (r *SnapshotRepository) ClaimOne(ctx context.Context, request ClaimOneRequest) (domain.ScrapeTarget, error) {
	if err := request.Ref.Validate(); err != nil {
		return domain.ScrapeTarget{}, err
	}
	if err := validateClaim(request.Now, request.WorkerID, request.LeaseDuration); err != nil {
		return domain.ScrapeTarget{}, err
	}
	row := r.pool.QueryRow(ctx, `
		WITH selected AS (
			SELECT target.id
			FROM scrape_targets AS target
			JOIN scrape_host_state AS host_state ON host_state.host = target.host
			WHERE target.host = $1
			  AND target.kind = $2
			  AND target.parent_key = $3
			  AND target.resource_key = $4
			  AND target.enabled = TRUE
			  AND (target.lease_expires_at IS NULL OR target.lease_expires_at <= $5::timestamptz)
			  AND (host_state.paused_until IS NULL OR host_state.paused_until <= $5::timestamptz)
			FOR UPDATE OF target SKIP LOCKED
		)
		UPDATE scrape_targets AS target
		SET lease_owner = $6,
			lease_generation = target.lease_generation + 1,
			lease_expires_at = $5::timestamptz + make_interval(secs => $7),
			last_attempt_at = $5::timestamptz,
			updated_at = $5::timestamptz
		FROM selected
		WHERE target.id = selected.id
		RETURNING `+targetReturningColumns,
		request.Ref.Host,
		request.Ref.Kind,
		request.Ref.ParentKey,
		request.Ref.ResourceKey,
		request.Now,
		request.WorkerID,
		durationSeconds(request.LeaseDuration),
	)
	target, err := scanScrapeTarget(row)
	if err == nil {
		return target, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.ScrapeTarget{}, fmt.Errorf("claim target: %w", err)
	}
	var exists bool
	if queryErr := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM scrape_targets
			WHERE host=$1 AND kind=$2 AND parent_key=$3 AND resource_key=$4
		)`,
		request.Ref.Host, request.Ref.Kind, request.Ref.ParentKey, request.Ref.ResourceKey,
	).Scan(&exists); queryErr != nil {
		return domain.ScrapeTarget{}, fmt.Errorf("claim target existence: %w", queryErr)
	}
	if !exists {
		return domain.ScrapeTarget{}, domain.ErrSnapshotNotFound
	}
	return domain.ScrapeTarget{}, domain.ErrTargetLeased
}

func scanScrapeTarget(row pgx.Row) (domain.ScrapeTarget, error) {
	var target domain.ScrapeTarget
	var kind string
	var attributes []byte
	var hash []byte
	var currentInterval int64
	var minInterval int64
	var maxInterval int64
	var maxServeAge int64
	var etag *string
	var lastModified *string
	var leaseOwner *string
	err := row.Scan(
		&target.ID,
		&target.Ref.Host,
		&kind,
		&target.Ref.ResourceKey,
		&target.Ref.ParentKey,
		&attributes,
		&target.MissingCount,
		&hash,
		&target.HasCurrentSnapshot,
		&target.CurrentVersion,
		&target.ValidationSeq,
		&currentInterval,
		&minInterval,
		&maxInterval,
		&maxServeAge,
		&target.NextRunAt,
		&target.LastValidatedAt,
		&target.ConsecutiveFailures,
		&target.RecentChanges,
		&etag,
		&lastModified,
		&leaseOwner,
		&target.LeaseGeneration,
		&target.LeaseExpiresAt,
	)
	if err != nil {
		return domain.ScrapeTarget{}, err
	}
	if leaseOwner != nil {
		target.LeaseOwner = *leaseOwner
	}
	target.Ref.Kind = domain.SnapshotKind(kind)
	target.Attributes = append(json.RawMessage(nil), attributes...)
	if len(hash) == 32 {
		copy(target.CurrentContentHash[:], hash)
	}
	target.CurrentInterval = time.Duration(currentInterval) * time.Second
	target.MinInterval = time.Duration(minInterval) * time.Second
	target.MaxInterval = time.Duration(maxInterval) * time.Second
	target.MaxServeAge = time.Duration(maxServeAge) * time.Second
	target.NextRunAt = target.NextRunAt.UTC()
	if target.LastValidatedAt != nil {
		value := target.LastValidatedAt.UTC()
		target.LastValidatedAt = &value
	}
	if target.LeaseExpiresAt != nil {
		value := target.LeaseExpiresAt.UTC()
		target.LeaseExpiresAt = &value
	}
	if etag != nil {
		target.Conditional.ETag = *etag
	}
	if lastModified != nil {
		target.Conditional.LastModified = *lastModified
	}
	return target, nil
}

func (r *SnapshotRepository) ReleaseLease(ctx context.Context, request ReleaseLeaseRequest) error {
	if request.TargetID <= 0 || request.LeaseGeneration <= 0 {
		return errors.New("release lease requires positive target ID and generation")
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE scrape_targets
		SET lease_owner = NULL,
			lease_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $1 AND lease_generation = $2`,
		request.TargetID,
		request.LeaseGeneration,
	)
	if err != nil {
		return fmt.Errorf("release target lease: %w", err)
	}
	return nil
}

func (r *SnapshotRepository) SetDueNow(ctx context.Context, ref domain.TargetRef, now time.Time) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE scrape_targets
		SET next_run_at = LEAST(next_run_at, $5),
			updated_at = $5
		WHERE host=$1 AND kind=$2 AND parent_key=$3 AND resource_key=$4`,
		ref.Host, ref.Kind, ref.ParentKey, ref.ResourceKey, now,
	)
	if err != nil {
		return fmt.Errorf("set target due: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrSnapshotNotFound
	}
	return nil
}

func (r *SnapshotRepository) Target(
	ctx context.Context,
	ref domain.TargetRef,
) (domain.ScrapeTarget, error) {
	if err := ref.Validate(); err != nil {
		return domain.ScrapeTarget{}, err
	}
	row := r.pool.QueryRow(ctx, `
		SELECT `+targetReturningColumns+`
		FROM scrape_targets AS target
		WHERE target.host=$1 AND target.kind=$2
		  AND target.parent_key=$3 AND target.resource_key=$4`,
		ref.Host,
		ref.Kind,
		ref.ParentKey,
		ref.ResourceKey,
	)
	target, err := scanScrapeTarget(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ScrapeTarget{}, domain.ErrSnapshotNotFound
	}
	if err != nil {
		return domain.ScrapeTarget{}, fmt.Errorf("read scrape target: %w", err)
	}
	return target, nil
}

func (r *SnapshotRepository) RescheduleLease(ctx context.Context, targetID, generation int64, nextRunAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE scrape_targets
		SET next_run_at=$3, lease_owner=NULL, lease_expires_at=NULL, updated_at=NOW()
		WHERE id=$1 AND lease_generation=$2`,
		targetID, generation, nextRunAt,
	)
	if err != nil {
		return fmt.Errorf("reschedule target lease: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

type lockedTarget struct {
	Ref               domain.TargetRef
	CurrentSnapshotID *int64
	CurrentVersion    int64
	ValidationSeq     int64
	LeaseGeneration   int64
	LeaseOwner        *string
	MaxServeAge       time.Duration
}

func (r *SnapshotRepository) Commit(ctx context.Context, input CommitInput) (CommitResult, error) {
	if err := validateCommit(input); err != nil {
		return CommitResult{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit snapshot: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if result, ok, err := existingCommitResult(ctx, tx, input.TargetID, input.LeaseGeneration); err != nil {
		return CommitResult{}, err
	} else if ok {
		if err := tx.Commit(ctx); err != nil {
			return CommitResult{}, fmt.Errorf("commit snapshot idempotent result: %w", err)
		}
		return result, nil
	}

	var target lockedTarget
	var kind string
	var currentSnapshotID *int64
	var leaseOwner *string
	var maxServeAgeSeconds int64
	err = tx.QueryRow(ctx, `
		SELECT host, kind, resource_key, parent_key, current_snapshot_id,
			current_version, validation_seq, lease_generation, lease_owner,
			max_serve_age_seconds
		FROM scrape_targets
		WHERE id=$1
		FOR UPDATE`,
		input.TargetID,
	).Scan(
		&target.Ref.Host,
		&kind,
		&target.Ref.ResourceKey,
		&target.Ref.ParentKey,
		&currentSnapshotID,
		&target.CurrentVersion,
		&target.ValidationSeq,
		&target.LeaseGeneration,
		&leaseOwner,
		&maxServeAgeSeconds,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommitResult{}, domain.ErrLeaseLost
	}
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit snapshot: lock target: %w", err)
	}
	target.Ref.Kind = domain.SnapshotKind(kind)
	target.CurrentSnapshotID = currentSnapshotID
	target.LeaseOwner = leaseOwner
	target.MaxServeAge = time.Duration(maxServeAgeSeconds) * time.Second
	if target.LeaseGeneration != input.LeaseGeneration {
		return CommitResult{}, domain.ErrLeaseLost
	}

	successful := successfulOutcome(input.Outcome)
	validationSeqAfter := target.ValidationSeq
	if successful {
		validationSeqAfter++
		if input.ValidationSeqAfter != nil && *input.ValidationSeqAfter != validationSeqAfter {
			return CommitResult{}, fmt.Errorf(
				"validation sequence must advance exactly once: have %d, input %d",
				target.ValidationSeq,
				*input.ValidationSeqAfter,
			)
		}
	}
	errorMessage := sanitizeErrorMessage(input.ErrorMessage)
	duration := input.FinishedAt.Sub(input.StartedAt)
	if duration < 0 {
		duration = 0
	}
	var validationSeqValue *int64
	if successful {
		validationSeqValue = &validationSeqAfter
	}
	var runID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO scrape_runs (
			target_id, worker_id, lease_generation, outcome,
			started_at, finished_at, http_status, duration_ms, bytes_read,
			error_kind, error_message, next_run_at, validation_seq_after
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13)
		RETURNING id`,
		input.TargetID,
		input.WorkerID,
		input.LeaseGeneration,
		input.Outcome,
		input.StartedAt,
		input.FinishedAt,
		input.HTTPStatus,
		duration.Milliseconds(),
		input.BytesRead,
		nullIfEmpty(input.ErrorKind),
		errorMessage,
		input.NextRunAt,
		validationSeqValue,
	).Scan(&runID)
	if err != nil {
		return CommitResult{}, fmt.Errorf("commit snapshot: insert run: %w", err)
	}

	var result CommitResult
	result.RunID = runID
	var snapshotID *int64
	version := target.CurrentVersion
	if successful && input.Changed {
		version++
		var insertedID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO scrape_snapshots (
				target_id, run_id, version, content_hash, payload, content_fetched_at
			)
			VALUES ($1,$2,$3,$4,$5,$6)
			RETURNING id`,
			input.TargetID,
			runID,
			version,
			input.ContentHash[:],
			input.Payload,
			input.FinishedAt,
		).Scan(&insertedID)
		if err != nil {
			return CommitResult{}, fmt.Errorf("commit snapshot: insert version: %w", err)
		}
		snapshotID = &insertedID
	}

	if successful {
		validatedAt := input.FinishedAt
		if input.ValidatedAt != nil {
			validatedAt = *input.ValidatedAt
		}
		etagExpression := "NULLIF($9, '')"
		lastModifiedExpression := "NULLIF($10, '')"
		cacheControlExpression := "NULLIF($11, '')"
		if input.Outcome == "not_modified" {
			etagExpression = "COALESCE(NULLIF($9, ''), etag)"
			lastModifiedExpression = "COALESCE(NULLIF($10, ''), last_modified)"
			cacheControlExpression = "COALESCE(NULLIF($11, ''), cache_control)"
		}
		query := `
			UPDATE scrape_targets
			SET next_run_at=$2,
				current_interval_seconds=$3,
				consecutive_failures=$4,
				recent_changes=$5,
				last_validated_at=$6,
				validation_seq=$7,
				etag=` + etagExpression + `,
				last_modified=` + lastModifiedExpression + `,
				cache_control=` + cacheControlExpression + `,
				current_snapshot_id=COALESCE($8, current_snapshot_id),
				current_version=CASE WHEN $8::bigint IS NULL THEN current_version ELSE $12 END,
				current_content_hash=CASE WHEN $8::bigint IS NULL THEN current_content_hash ELSE $13 END,
				last_content_change_at=CASE WHEN $8::bigint IS NULL THEN last_content_change_at ELSE $6 END,
				lease_owner=NULL,
				lease_expires_at=NULL,
				updated_at=$6
			WHERE id=$1 AND lease_generation=$14`
		tag, updateErr := tx.Exec(ctx, query,
			input.TargetID,
			input.NextRunAt,
			durationSeconds(input.CurrentInterval),
			input.ConsecutiveFailures,
			trimBoolHistory(input.RecentChanges),
			validatedAt,
			validationSeqAfter,
			snapshotID,
			input.ETag,
			input.LastModified,
			input.CacheControl,
			version,
			nullableHash(input.Changed, input.ContentHash),
			input.LeaseGeneration,
		)
		if updateErr != nil {
			return CommitResult{}, fmt.Errorf("commit snapshot: update target success: %w", updateErr)
		}
		if tag.RowsAffected() != 1 {
			return CommitResult{}, domain.ErrLeaseLost
		}
	} else {
		tag, updateErr := tx.Exec(ctx, `
			UPDATE scrape_targets
			SET next_run_at=$2,
				current_interval_seconds=$3,
				consecutive_failures=$4,
				recent_changes=$5,
				lease_owner=NULL,
				lease_expires_at=NULL,
				updated_at=$6
			WHERE id=$1 AND lease_generation=$7`,
			input.TargetID,
			input.NextRunAt,
			durationSeconds(input.CurrentInterval),
			input.ConsecutiveFailures,
			trimBoolHistory(input.RecentChanges),
			input.FinishedAt,
			input.LeaseGeneration,
		)
		if updateErr != nil {
			return CommitResult{}, fmt.Errorf("commit snapshot: update target failure: %w", updateErr)
		}
		if tag.RowsAffected() != 1 {
			return CommitResult{}, domain.ErrLeaseLost
		}
	}

	for _, seed := range input.Discovered {
		if err := upsertSeed(ctx, tx, seed); err != nil {
			return CommitResult{}, err
		}
	}
	if successful && input.Changed {
		if err := applyChildPresence(ctx, tx, target.Ref, input.SeenChildRefs); err != nil {
			return CommitResult{}, err
		}
	}

	if snapshotID != nil {
		validatedAt := input.FinishedAt
		if input.ValidatedAt != nil {
			validatedAt = *input.ValidatedAt
		}
		snapshot := &domain.Snapshot{
			ID:               *snapshotID,
			TargetID:         input.TargetID,
			Ref:              target.Ref,
			Version:          version,
			ValidationSeq:    validationSeqAfter,
			ContentHash:      input.ContentHash,
			Payload:          append(json.RawMessage(nil), input.Payload...),
			ContentFetchedAt: input.FinishedAt,
			ValidatedAt:      validatedAt,
			NextRunAt:        input.NextRunAt,
			MaxServeAge:      target.MaxServeAge,
		}
		metadata := &domain.SnapshotMetadata{
			Kind:          target.Ref.Kind,
			ResourceKey:   target.Ref.ResourceKey,
			ParentKey:     target.Ref.ParentKey,
			Version:       version,
			ValidationSeq: validationSeqAfter,
			ValidatedAt:   validatedAt,
			Stale:         validatedAt.After(input.NextRunAt),
		}
		notification, marshalErr := json.Marshal(metadata)
		if marshalErr != nil {
			return CommitResult{}, fmt.Errorf("commit snapshot: marshal notification: %w", marshalErr)
		}
		if len(notification) >= 8000 {
			return CommitResult{}, errors.New("commit snapshot: notification payload too large")
		}
		if _, err := tx.Exec(ctx, `SELECT pg_notify('snapshot_committed', $1)`, string(notification)); err != nil {
			return CommitResult{}, fmt.Errorf("commit snapshot: notify: %w", err)
		}
		result.Snapshot = snapshot
		result.Metadata = metadata
	}

	if err := tx.Commit(ctx); err != nil {
		return CommitResult{}, fmt.Errorf("commit snapshot transaction: %w", err)
	}
	return result, nil
}

func validateCommit(input CommitInput) error {
	if input.TargetID <= 0 || input.LeaseGeneration <= 0 {
		return errors.New("commit requires positive target ID and lease generation")
	}
	if strings.TrimSpace(input.WorkerID) == "" {
		return errors.New("commit worker ID is required")
	}
	switch input.Outcome {
	case "changed", "unchanged", "not_modified", "rate_limited", "auth_error",
		"transient_error", "not_found", "permanent_error", "invalid_payload", "canceled":
	default:
		return fmt.Errorf("invalid scrape outcome %q", input.Outcome)
	}
	if input.StartedAt.IsZero() || input.FinishedAt.IsZero() || input.NextRunAt.IsZero() {
		return errors.New("commit timestamps are required")
	}
	if input.BytesRead < 0 || input.ConsecutiveFailures < 0 {
		return errors.New("commit counters must be non-negative")
	}
	if input.CurrentInterval <= 0 {
		return errors.New("commit interval must be positive")
	}
	if input.Changed {
		if input.Outcome != "changed" {
			return errors.New("only changed outcome may insert content")
		}
		if domain.IsZeroContentHash(input.ContentHash) {
			return errors.New("changed commit content hash is required")
		}
		if len(input.Payload) == 0 || !json.Valid(input.Payload) {
			return errors.New("changed commit payload must be valid JSON")
		}
	}
	return nil
}

func successfulOutcome(outcome string) bool {
	return outcome == "changed" || outcome == "unchanged" || outcome == "not_modified"
}

func nullableHash(changed bool, hash [32]byte) any {
	if !changed {
		return nil
	}
	return hash[:]
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func trimBoolHistory(history []bool) []bool {
	copied := make([]bool, len(history))
	copy(copied, history)
	if len(copied) > 10 {
		copied = copied[len(copied)-10:]
	}
	return copied
}

var secretAssignment = regexp.MustCompile(`(?i)(asp\.net_sessionid|cookie|authorization)\s*[:=]\s*[^\s,;]+`)

func sanitizeErrorMessage(message string) string {
	message = secretAssignment.ReplaceAllString(message, "$1=<redacted>")
	if utf8.RuneCountInString(message) <= 512 {
		return message
	}
	runes := []rune(message)
	return string(runes[:512])
}

func existingCommitResult(
	ctx context.Context,
	tx pgx.Tx,
	targetID int64,
	generation int64,
) (CommitResult, bool, error) {
	var runID int64
	var outcome string
	err := tx.QueryRow(ctx, `
		SELECT id, outcome
		FROM scrape_runs
		WHERE target_id=$1 AND lease_generation=$2`,
		targetID,
		generation,
	).Scan(&runID, &outcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommitResult{}, false, nil
	}
	if err != nil {
		return CommitResult{}, false, fmt.Errorf("commit snapshot: read existing run: %w", err)
	}
	result := CommitResult{RunID: runID}
	if outcome != "changed" {
		return result, true, nil
	}
	snapshot, err := snapshotByRun(ctx, tx, runID)
	if err != nil {
		return CommitResult{}, false, err
	}
	result.Snapshot = &snapshot
	result.Metadata = &domain.SnapshotMetadata{
		Kind:          snapshot.Ref.Kind,
		ResourceKey:   snapshot.Ref.ResourceKey,
		ParentKey:     snapshot.Ref.ParentKey,
		Version:       snapshot.Version,
		ValidationSeq: snapshot.ValidationSeq,
		ValidatedAt:   snapshot.ValidatedAt,
		Stale:         snapshot.Stale(time.Now().UTC()),
	}
	return result, true, nil
}

func snapshotByRun(ctx context.Context, tx pgx.Tx, runID int64) (domain.Snapshot, error) {
	var snapshot domain.Snapshot
	var kind string
	var hash []byte
	var payload []byte
	var maxServeAgeSeconds int64
	err := tx.QueryRow(ctx, `
		SELECT snapshot.id, snapshot.target_id, target.host, target.kind,
			target.resource_key, target.parent_key, snapshot.version,
			target.validation_seq, snapshot.content_hash, snapshot.payload,
			snapshot.content_fetched_at, target.last_validated_at,
			target.next_run_at, target.max_serve_age_seconds
		FROM scrape_snapshots AS snapshot
		JOIN scrape_targets AS target ON target.id=snapshot.target_id
		WHERE snapshot.run_id=$1`,
		runID,
	).Scan(
		&snapshot.ID,
		&snapshot.TargetID,
		&snapshot.Ref.Host,
		&kind,
		&snapshot.Ref.ResourceKey,
		&snapshot.Ref.ParentKey,
		&snapshot.Version,
		&snapshot.ValidationSeq,
		&hash,
		&payload,
		&snapshot.ContentFetchedAt,
		&snapshot.ValidatedAt,
		&snapshot.NextRunAt,
		&maxServeAgeSeconds,
	)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("read snapshot by run: %w", err)
	}
	snapshot.Ref.Kind = domain.SnapshotKind(kind)
	copy(snapshot.ContentHash[:], hash)
	snapshot.Payload = append(json.RawMessage(nil), payload...)
	snapshot.MaxServeAge = time.Duration(maxServeAgeSeconds) * time.Second
	snapshot.ContentFetchedAt = snapshot.ContentFetchedAt.UTC()
	snapshot.ValidatedAt = snapshot.ValidatedAt.UTC()
	snapshot.NextRunAt = snapshot.NextRunAt.UTC()
	return snapshot, nil
}

func applyChildPresence(
	ctx context.Context,
	tx pgx.Tx,
	parent domain.TargetRef,
	seen []domain.TargetRef,
) error {
	var childKind domain.SnapshotKind
	var childParent string
	switch parent.Kind {
	case domain.SnapshotCourseCatalog:
		childKind = domain.SnapshotCourseDetail
		childParent = ""
	case domain.SnapshotCourseDetail:
		childKind = domain.SnapshotSessionDetail
		childParent = parent.ResourceKey
	default:
		return nil
	}
	seenKeys := make([]string, 0, len(seen))
	for _, ref := range seen {
		if ref.Host != parent.Host || ref.Kind != childKind || ref.ParentKey != childParent {
			continue
		}
		seenKeys = append(seenKeys, ref.ResourceKey)
	}
	if len(seenKeys) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE scrape_targets
			SET missing_count=0, enabled=TRUE, updated_at=NOW()
			WHERE host=$1 AND kind=$2 AND parent_key=$3
			  AND resource_key = ANY($4)`,
			parent.Host, childKind, childParent, seenKeys,
		); err != nil {
			return fmt.Errorf("reset discovered child presence: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scrape_targets
		SET missing_count=LEAST(2, missing_count + 1),
			enabled=CASE WHEN missing_count >= 1 THEN FALSE ELSE enabled END,
			updated_at=NOW()
		WHERE host=$1 AND kind=$2 AND parent_key=$3
		  AND NOT (resource_key = ANY($4))`,
		parent.Host, childKind, childParent, seenKeys,
	); err != nil {
		return fmt.Errorf("update missing child presence: %w", err)
	}
	return nil
}

func (r *SnapshotRepository) Current(ctx context.Context, ref domain.TargetRef) (domain.Snapshot, error) {
	if err := ref.Validate(); err != nil {
		return domain.Snapshot{}, err
	}
	var snapshot domain.Snapshot
	var hash []byte
	var payload []byte
	var maxServeAgeSeconds int64
	err := r.pool.QueryRow(ctx, `
		SELECT snapshot.id, target.id, snapshot.version, target.validation_seq,
			snapshot.content_hash, snapshot.payload, snapshot.content_fetched_at,
			target.last_validated_at, target.next_run_at, target.max_serve_age_seconds
		FROM scrape_targets AS target
		JOIN scrape_snapshots AS snapshot ON snapshot.id=target.current_snapshot_id
		WHERE target.host=$1 AND target.kind=$2
		  AND target.parent_key=$3 AND target.resource_key=$4`,
		ref.Host,
		ref.Kind,
		ref.ParentKey,
		ref.ResourceKey,
	).Scan(
		&snapshot.ID,
		&snapshot.TargetID,
		&snapshot.Version,
		&snapshot.ValidationSeq,
		&hash,
		&payload,
		&snapshot.ContentFetchedAt,
		&snapshot.ValidatedAt,
		&snapshot.NextRunAt,
		&maxServeAgeSeconds,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Snapshot{}, domain.ErrSnapshotNotFound
	}
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("read current snapshot: %w", err)
	}
	snapshot.Ref = ref
	copy(snapshot.ContentHash[:], hash)
	snapshot.Payload = append(json.RawMessage(nil), payload...)
	snapshot.MaxServeAge = time.Duration(maxServeAgeSeconds) * time.Second
	snapshot.ContentFetchedAt = snapshot.ContentFetchedAt.UTC()
	snapshot.ValidatedAt = snapshot.ValidatedAt.UTC()
	snapshot.NextRunAt = snapshot.NextRunAt.UTC()
	return snapshot, nil
}

func (r *SnapshotRepository) Metadata(
	ctx context.Context,
	ref domain.TargetRef,
	now time.Time,
) (domain.SnapshotMetadata, error) {
	snapshot, err := r.Current(ctx, ref)
	if err != nil {
		return domain.SnapshotMetadata{}, err
	}
	return domain.SnapshotMetadata{
		Kind:          ref.Kind,
		ResourceKey:   ref.ResourceKey,
		ParentKey:     ref.ParentKey,
		Version:       snapshot.Version,
		ValidationSeq: snapshot.ValidationSeq,
		ValidatedAt:   snapshot.ValidatedAt,
		Stale:         snapshot.Stale(now),
	}, nil
}

func (r *SnapshotRepository) ListMetadata(
	ctx context.Context,
	now time.Time,
) ([]domain.SnapshotMetadata, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT target.kind, target.resource_key, target.parent_key,
			target.current_version, target.validation_seq, target.last_validated_at,
			target.next_run_at
		FROM scrape_targets AS target
		WHERE target.current_snapshot_id IS NOT NULL
		ORDER BY target.kind, target.parent_key, target.resource_key`)
	if err != nil {
		return nil, fmt.Errorf("list snapshot metadata: %w", err)
	}
	defer rows.Close()
	metadata := make([]domain.SnapshotMetadata, 0)
	for rows.Next() {
		var item domain.SnapshotMetadata
		var kind string
		var nextRunAt time.Time
		if err := rows.Scan(
			&kind,
			&item.ResourceKey,
			&item.ParentKey,
			&item.Version,
			&item.ValidationSeq,
			&item.ValidatedAt,
			&nextRunAt,
		); err != nil {
			return nil, fmt.Errorf("list snapshot metadata: scan: %w", err)
		}
		item.Kind = domain.SnapshotKind(kind)
		item.ValidatedAt = item.ValidatedAt.UTC()
		nextRunAt = nextRunAt.UTC()
		item.Stale = now.After(nextRunAt)
		metadata = append(metadata, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list snapshot metadata: rows: %w", err)
	}
	return metadata, nil
}

func (r *SnapshotRepository) AnyOverdue(
	ctx context.Context,
	refs []domain.TargetRef,
	now time.Time,
) (bool, error) {
	for _, ref := range refs {
		var overdue bool
		err := r.pool.QueryRow(ctx, `
			SELECT next_run_at < $5
			FROM scrape_targets
			WHERE host=$1 AND kind=$2 AND parent_key=$3 AND resource_key=$4
			  AND current_snapshot_id IS NOT NULL`,
			ref.Host,
			ref.Kind,
			ref.ParentKey,
			ref.ResourceKey,
			now,
		).Scan(&overdue)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, domain.ErrSnapshotNotFound
		}
		if err != nil {
			return false, fmt.Errorf("check snapshot overdue: %w", err)
		}
		if overdue {
			return true, nil
		}
	}
	return false, nil
}

func (r *SnapshotRepository) CountDue(ctx context.Context, now time.Time) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM scrape_targets AS target
		JOIN scrape_host_state AS host_state ON host_state.host=target.host
		WHERE target.enabled=TRUE
		  AND target.next_run_at <= $1
		  AND (target.lease_expires_at IS NULL OR target.lease_expires_at <= $1)
		  AND (host_state.paused_until IS NULL OR host_state.paused_until <= $1)`,
		now,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count due targets: %w", err)
	}
	return count, nil
}

func (r *SnapshotRepository) ScraperStatus(
	ctx context.Context,
	host string,
	now time.Time,
) (ScraperStatus, error) {
	status := ScraperStatus{
		OldestValidationAgeSeconds: make(map[domain.SnapshotKind]int64),
		OldestSnapshotAgeSeconds:   make(map[domain.SnapshotKind]int64),
		DueByKind:                  make(map[domain.SnapshotKind]int),
	}
	if strings.TrimSpace(host) == "" {
		return status, errors.New("scraper status host is required")
	}
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (
				WHERE enabled=TRUE
				  AND next_run_at <= $2
				  AND (lease_expires_at IS NULL OR lease_expires_at <= $2)
			),
			COUNT(*) FILTER (
				WHERE lease_expires_at > $2
			),
			COUNT(*) FILTER (
				WHERE consecutive_failures > 0
			),
			COUNT(*) FILTER (
				WHERE current_snapshot_id IS NOT NULL
				  AND (
					last_validated_at IS NULL
					OR last_validated_at
						+ max_serve_age_seconds * INTERVAL '1 second' < $2
				  )
			)
		FROM scrape_targets
		WHERE host=$1`,
		host,
		now,
	).Scan(
		&status.Due,
		&status.Leased,
		&status.Failed,
		&status.ExpiredCurrent,
	)
	if err != nil {
		return status, fmt.Errorf("read scraper target status: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT kind, COUNT(*)
		FROM scrape_targets
		WHERE host=$1
		  AND enabled=TRUE
		  AND next_run_at <= $2
		  AND (lease_expires_at IS NULL OR lease_expires_at <= $2)
		GROUP BY kind`,
		host,
		now,
	)
	if err != nil {
		return status, fmt.Errorf("read scraper due counts: %w", err)
	}
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			rows.Close()
			return status, fmt.Errorf("read scraper due counts: scan: %w", err)
		}
		status.DueByKind[domain.SnapshotKind(kind)] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return status, fmt.Errorf("read scraper due counts: rows: %w", err)
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `
		SELECT target.kind,
			FLOOR(MAX(EXTRACT(EPOCH FROM ($2 - target.last_validated_at))))::bigint,
			FLOOR(MAX(EXTRACT(EPOCH FROM ($2 - snapshot.content_fetched_at))))::bigint
		FROM scrape_targets AS target
		JOIN scrape_snapshots AS snapshot
		  ON snapshot.id=target.current_snapshot_id
		WHERE target.host=$1
		  AND target.last_validated_at IS NOT NULL
		GROUP BY target.kind`,
		host,
		now,
	)
	if err != nil {
		return status, fmt.Errorf("read scraper validation ages: %w", err)
	}
	for rows.Next() {
		var kind string
		var validationSeconds int64
		var snapshotSeconds int64
		if err := rows.Scan(&kind, &validationSeconds, &snapshotSeconds); err != nil {
			rows.Close()
			return status, fmt.Errorf("read scraper validation ages: scan: %w", err)
		}
		snapshotKind := domain.SnapshotKind(kind)
		status.OldestValidationAgeSeconds[snapshotKind] = validationSeconds
		status.OldestSnapshotAgeSeconds[snapshotKind] = snapshotSeconds
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return status, fmt.Errorf("read scraper validation ages: rows: %w", err)
	}
	rows.Close()

	var pausedUntil *time.Time
	err = r.pool.QueryRow(ctx, `
		SELECT current_requests_per_second::double precision,
			current_concurrency,
			paused_until
		FROM scrape_host_state
		WHERE host=$1`,
		host,
	).Scan(
		&status.HostRequestsPerSecond,
		&status.HostConcurrency,
		&pausedUntil,
	)
	if err != nil {
		return status, fmt.Errorf("read scraper host status: %w", err)
	}
	if pausedUntil != nil {
		paused := pausedUntil.UTC()
		status.HostPausedUntil = &paused
	}

	err = r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE expires_at > $2),
			COUNT(*) FILTER (WHERE expires_at <= $2)
		FROM scrape_host_permits
		WHERE host=$1`,
		host,
		now,
	).Scan(&status.ActivePermits, &status.ExpiredPermits)
	if err != nil {
		return status, fmt.Errorf("read scraper permit status: %w", err)
	}
	return status, nil
}

func (r *SnapshotRepository) AcquireHostPermit(
	ctx context.Context,
	request AcquireHostPermitRequest,
) (domain.PermitDecision, error) {
	if strings.TrimSpace(request.Host) == "" || strings.TrimSpace(request.WorkerID) == "" {
		return domain.PermitDecision{}, errors.New("host permit requires host and worker ID")
	}
	if request.TargetID <= 0 || request.LeaseGeneration <= 0 || request.Now.IsZero() || request.TTL <= 0 {
		return domain.PermitDecision{}, errors.New("host permit request is invalid")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.PermitDecision{}, fmt.Errorf("acquire host permit: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existing domain.HostPermit
	err = tx.QueryRow(ctx, `
		SELECT id, host, target_id, lease_generation, expires_at
		FROM scrape_host_permits
		WHERE target_id=$1 AND lease_generation=$2 AND expires_at > $3`,
		request.TargetID,
		request.LeaseGeneration,
		request.Now,
	).Scan(
		&existing.ID,
		&existing.Host,
		&existing.TargetID,
		&existing.LeaseGeneration,
		&existing.ExpiresAt,
	)
	if err == nil {
		existing.ExpiresAt = existing.ExpiresAt.UTC()
		if err := tx.Commit(ctx); err != nil {
			return domain.PermitDecision{}, fmt.Errorf("acquire existing host permit: %w", err)
		}
		return domain.PermitDecision{Permit: &existing}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.PermitDecision{}, fmt.Errorf("acquire host permit: existing: %w", err)
	}

	state, err := lockHostState(ctx, tx, request.Host)
	if err != nil {
		return domain.PermitDecision{}, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM scrape_host_permits
		WHERE host=$1 AND expires_at <= $2`,
		request.Host,
		request.Now,
	); err != nil {
		return domain.PermitDecision{}, fmt.Errorf("acquire host permit: expire permits: %w", err)
	}

	var targetHost string
	var targetGeneration int64
	err = tx.QueryRow(ctx, `
		SELECT host, lease_generation
		FROM scrape_targets
		WHERE id=$1`,
		request.TargetID,
	).Scan(&targetHost, &targetGeneration)
	if errors.Is(err, pgx.ErrNoRows) || targetHost != request.Host || targetGeneration != request.LeaseGeneration {
		return domain.PermitDecision{}, domain.ErrLeaseLost
	}
	if err != nil {
		return domain.PermitDecision{}, fmt.Errorf("acquire host permit: target fence: %w", err)
	}

	if state.PausedUntil != nil && state.PausedUntil.After(request.Now) {
		decision := domain.PermitDecision{RetryAt: state.PausedUntil.UTC(), Paused: true}
		if err := tx.Commit(ctx); err != nil {
			return domain.PermitDecision{}, fmt.Errorf("acquire paused host permit: %w", err)
		}
		return decision, nil
	}

	var active int
	var earliest *time.Time
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*), MIN(expires_at)
		FROM scrape_host_permits
		WHERE host=$1 AND expires_at > $2`,
		request.Host,
		request.Now,
	).Scan(&active, &earliest)
	if err != nil {
		return domain.PermitDecision{}, fmt.Errorf("acquire host permit: count active: %w", err)
	}

	refilled := refillTokens(state, request.Now)
	if active >= state.CurrentConcurrency {
		if err := persistTokens(ctx, tx, request.Host, refilled, request.Now); err != nil {
			return domain.PermitDecision{}, err
		}
		retryAt := request.Now.Add(request.TTL)
		if earliest != nil {
			retryAt = earliest.UTC()
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.PermitDecision{}, fmt.Errorf("acquire concurrency-blocked permit: %w", err)
		}
		return domain.PermitDecision{RetryAt: retryAt}, nil
	}
	if refilled < 1 {
		if err := persistTokens(ctx, tx, request.Host, refilled, request.Now); err != nil {
			return domain.PermitDecision{}, err
		}
		waitSeconds := (1 - refilled) / state.CurrentRequestsPerSecond
		retryAt := request.Now.Add(time.Duration(waitSeconds * float64(time.Second)))
		if err := tx.Commit(ctx); err != nil {
			return domain.PermitDecision{}, fmt.Errorf("acquire rate-blocked permit: %w", err)
		}
		return domain.PermitDecision{RetryAt: retryAt}, nil
	}

	available := refilled - 1
	if err := persistTokens(ctx, tx, request.Host, available, request.Now); err != nil {
		return domain.PermitDecision{}, err
	}
	var permit domain.HostPermit
	permit.ExpiresAt = request.Now.Add(request.TTL)
	err = tx.QueryRow(ctx, `
		INSERT INTO scrape_host_permits (
			host, target_id, worker_id, lease_generation, acquired_at, expires_at
		)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, host, target_id, lease_generation, expires_at`,
		request.Host,
		request.TargetID,
		request.WorkerID,
		request.LeaseGeneration,
		request.Now,
		permit.ExpiresAt,
	).Scan(
		&permit.ID,
		&permit.Host,
		&permit.TargetID,
		&permit.LeaseGeneration,
		&permit.ExpiresAt,
	)
	if err != nil {
		return domain.PermitDecision{}, fmt.Errorf("acquire host permit: insert: %w", err)
	}
	permit.ExpiresAt = permit.ExpiresAt.UTC()
	if err := tx.Commit(ctx); err != nil {
		return domain.PermitDecision{}, fmt.Errorf("acquire host permit: commit: %w", err)
	}
	return domain.PermitDecision{Permit: &permit}, nil
}

func lockHostState(ctx context.Context, tx pgx.Tx, host string) (HostState, error) {
	var state HostState
	err := tx.QueryRow(ctx, `
		SELECT host,
			baseline_requests_per_second::float8,
			current_requests_per_second::float8,
			burst,
			available_tokens::float8,
			tokens_updated_at,
			baseline_concurrency,
			current_concurrency,
			consecutive_429s,
			last_429_at,
			healthy_streak,
			paused_until
		FROM scrape_host_state
		WHERE host=$1
		FOR UPDATE`,
		host,
	).Scan(
		&state.Host,
		&state.BaselineRequestsPerSecond,
		&state.CurrentRequestsPerSecond,
		&state.Burst,
		&state.AvailableTokens,
		&state.TokensUpdatedAt,
		&state.BaselineConcurrency,
		&state.CurrentConcurrency,
		&state.Consecutive429s,
		&state.Last429At,
		&state.HealthyStreak,
		&state.PausedUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return HostState{}, fmt.Errorf("host state %q not found", host)
	}
	if err != nil {
		return HostState{}, fmt.Errorf("lock host state: %w", err)
	}
	normalizeHostStateTimes(&state)
	return state, nil
}

func normalizeHostStateTimes(state *HostState) {
	state.TokensUpdatedAt = state.TokensUpdatedAt.UTC()
	if state.Last429At != nil {
		value := state.Last429At.UTC()
		state.Last429At = &value
	}
	if state.PausedUntil != nil {
		value := state.PausedUntil.UTC()
		state.PausedUntil = &value
	}
}

func refillTokens(state HostState, now time.Time) float64 {
	elapsed := now.Sub(state.TokensUpdatedAt).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	refilled := state.AvailableTokens + elapsed*state.CurrentRequestsPerSecond
	if refilled > float64(state.Burst) {
		refilled = float64(state.Burst)
	}
	if refilled < 0 {
		return 0
	}
	return refilled
}

func persistTokens(ctx context.Context, tx pgx.Tx, host string, available float64, now time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE scrape_host_state
		SET available_tokens=$2, tokens_updated_at=$3, updated_at=$3
		WHERE host=$1`,
		host,
		available,
		now,
	)
	if err != nil {
		return fmt.Errorf("persist host tokens: %w", err)
	}
	return nil
}

func (r *SnapshotRepository) ReleaseHostPermit(ctx context.Context, permitID int64) error {
	if permitID <= 0 {
		return nil
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM scrape_host_permits WHERE id=$1`, permitID); err != nil {
		return fmt.Errorf("release host permit: %w", err)
	}
	return nil
}

func (r *SnapshotRepository) ObserveHost(ctx context.Context, observation domain.HostObservation) error {
	if strings.TrimSpace(observation.Host) == "" || observation.ObservedAt.IsZero() {
		return errors.New("host observation requires host and observed time")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("observe host: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := lockHostState(ctx, tx, observation.Host)
	if err != nil {
		return err
	}
	available := refillTokens(state, observation.ObservedAt)

	switch observation.Outcome {
	case "rate_limited":
		pause := 15 * time.Minute
		if observation.RetryAfter > pause {
			pause = observation.RetryAfter
		}
		if state.Last429At != nil && observation.ObservedAt.Sub(*state.Last429At) <= time.Hour {
			if pause < time.Hour {
				pause = time.Hour
			}
		}
		currentRPS := state.CurrentRequestsPerSecond / 2
		if currentRPS < 0.25 {
			currentRPS = 0.25
		}
		pausedUntil := observation.ObservedAt.Add(pause)
		_, err = tx.Exec(ctx, `
			UPDATE scrape_host_state
			SET current_requests_per_second=$2,
				current_concurrency=1,
				available_tokens=LEAST($3::numeric, burst::numeric),
				tokens_updated_at=$4,
				consecutive_429s=consecutive_429s+1,
				last_429_at=$4,
				healthy_streak=0,
				paused_until=$5,
				updated_at=$4
			WHERE host=$1`,
			observation.Host,
			currentRPS,
			available,
			observation.ObservedAt,
			pausedUntil,
		)
	case "auth_error":
		pausedUntil := observation.ObservedAt.Add(15 * time.Minute)
		_, err = tx.Exec(ctx, `
			UPDATE scrape_host_state
			SET available_tokens=LEAST($2::numeric, burst::numeric),
				tokens_updated_at=$3,
				healthy_streak=0,
				paused_until=$4,
				updated_at=$3
			WHERE host=$1`,
			observation.Host,
			available,
			observation.ObservedAt,
			pausedUntil,
		)
	case "changed", "unchanged", "not_modified":
		healthy := state.HealthyStreak + 1
		currentRPS := state.CurrentRequestsPerSecond
		currentConcurrency := state.CurrentConcurrency
		if healthy >= 20 {
			currentRPS += 0.25
			if currentRPS > state.BaselineRequestsPerSecond {
				currentRPS = state.BaselineRequestsPerSecond
			}
			currentConcurrency++
			if currentConcurrency > state.BaselineConcurrency {
				currentConcurrency = state.BaselineConcurrency
			}
			healthy = 0
		}
		var pausedUntil *time.Time
		if state.PausedUntil != nil && state.PausedUntil.After(observation.ObservedAt) {
			pausedUntil = state.PausedUntil
		}
		_, err = tx.Exec(ctx, `
			UPDATE scrape_host_state
			SET current_requests_per_second=$2,
				current_concurrency=$3,
				available_tokens=LEAST($4::numeric, burst::numeric),
				tokens_updated_at=$5,
				consecutive_429s=0,
				healthy_streak=$6,
				paused_until=$7,
				updated_at=$5
			WHERE host=$1`,
			observation.Host,
			currentRPS,
			currentConcurrency,
			available,
			observation.ObservedAt,
			healthy,
			pausedUntil,
		)
	default:
		_, err = tx.Exec(ctx, `
			UPDATE scrape_host_state
			SET available_tokens=LEAST($2::numeric, burst::numeric),
				tokens_updated_at=$3,
				healthy_streak=0,
				updated_at=$3
			WHERE host=$1`,
			observation.Host,
			available,
			observation.ObservedAt,
		)
	}
	if err != nil {
		return fmt.Errorf("observe host: update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("observe host: commit: %w", err)
	}
	return nil
}

func (r *SnapshotRepository) HostState(ctx context.Context, host string) (HostState, error) {
	var state HostState
	err := r.pool.QueryRow(ctx, `
		SELECT host,
			baseline_requests_per_second::float8,
			current_requests_per_second::float8,
			burst,
			available_tokens::float8,
			tokens_updated_at,
			baseline_concurrency,
			current_concurrency,
			consecutive_429s,
			last_429_at,
			healthy_streak,
			paused_until
		FROM scrape_host_state
		WHERE host=$1`,
		host,
	).Scan(
		&state.Host,
		&state.BaselineRequestsPerSecond,
		&state.CurrentRequestsPerSecond,
		&state.Burst,
		&state.AvailableTokens,
		&state.TokensUpdatedAt,
		&state.BaselineConcurrency,
		&state.CurrentConcurrency,
		&state.Consecutive429s,
		&state.Last429At,
		&state.HealthyStreak,
		&state.PausedUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return HostState{}, fmt.Errorf("host state %q not found", host)
	}
	if err != nil {
		return HostState{}, fmt.Errorf("read host state: %w", err)
	}
	normalizeHostStateTimes(&state)
	return state, nil
}

func (r *SnapshotRepository) Prune(ctx context.Context, request PruneRequest) (PruneResult, error) {
	if request.BatchSize <= 0 || request.BatchSize > 5000 {
		return PruneResult{}, errors.New("prune batch size must be between 1 and 5000")
	}
	if request.SnapshotRetention <= 0 || request.RunRetention <= 0 || request.Now.IsZero() {
		return PruneResult{}, errors.New("prune retention and time must be positive")
	}
	var result PruneResult
	for {
		tag, err := r.pool.Exec(ctx, `
			WITH ranked AS (
				SELECT snapshot.id, snapshot.target_id,
					ROW_NUMBER() OVER (
						PARTITION BY snapshot.target_id
						ORDER BY snapshot.version DESC
					) AS position
				FROM scrape_snapshots AS snapshot
			),
			candidates AS (
				SELECT ranked.id
				FROM ranked
				JOIN scrape_snapshots AS snapshot ON snapshot.id=ranked.id
				JOIN scrape_targets AS target ON target.id=ranked.target_id
				WHERE ranked.position > 3
				  AND snapshot.id IS DISTINCT FROM target.current_snapshot_id
				  AND snapshot.content_fetched_at < $1
				ORDER BY snapshot.content_fetched_at, snapshot.id
				LIMIT $2
			)
			DELETE FROM scrape_snapshots
			WHERE id IN (SELECT id FROM candidates)`,
			request.Now.Add(-request.SnapshotRetention),
			request.BatchSize,
		)
		if err != nil {
			return result, fmt.Errorf("prune snapshots: %w", err)
		}
		deleted := int(tag.RowsAffected())
		result.SnapshotsDeleted += deleted
		if deleted < request.BatchSize {
			break
		}
	}
	for {
		tag, err := r.pool.Exec(ctx, `
			WITH candidates AS (
				SELECT run.id
				FROM scrape_runs AS run
				WHERE run.finished_at < $1
				  AND NOT EXISTS (
					SELECT 1 FROM scrape_snapshots AS snapshot WHERE snapshot.run_id=run.id
				  )
				ORDER BY run.finished_at, run.id
				LIMIT $2
			)
			DELETE FROM scrape_runs
			WHERE id IN (SELECT id FROM candidates)`,
			request.Now.Add(-request.RunRetention),
			request.BatchSize,
		)
		if err != nil {
			return result, fmt.Errorf("prune runs: %w", err)
		}
		deleted := int(tag.RowsAffected())
		result.RunsDeleted += deleted
		if deleted < request.BatchSize {
			break
		}
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM scrape_host_permits WHERE expires_at <= $1`, request.Now)
	if err != nil {
		return result, fmt.Errorf("prune host permits: %w", err)
	}
	result.PermitsDeleted = int(tag.RowsAffected())
	return result, nil
}
