package domain

import (
	"encoding/json"
	"time"
)

// DataQualityState describes how trustworthy a snapshot is. It is the
// correctness contract exposed to every API consumer: verified data is never
// silently replaced by unverified data, and stale data is always marked.
type DataQualityState string

const (
	DataQualityVerifiedFresh DataQualityState = "verified_fresh"
	DataQualityVerifiedStale DataQualityState = "verified_stale"
	DataQualityDegraded      DataQualityState = "degraded"
	DataQualityUnavailable   DataQualityState = "unavailable"
)

func (s DataQualityState) Valid() bool {
	switch s {
	case DataQualityVerifiedFresh,
		DataQualityVerifiedStale,
		DataQualityDegraded,
		DataQualityUnavailable:
		return true
	default:
		return false
	}
}

type ValidationSeverity string

const (
	SeverityInfo    ValidationSeverity = "info"
	SeverityWarning ValidationSeverity = "warning"
	SeverityError   ValidationSeverity = "error"
	SeverityFatal   ValidationSeverity = "fatal"
)

type ValidationViolation struct {
	Code     string             `json:"code"`
	Severity ValidationSeverity `json:"severity"`
	Path     string             `json:"path,omitempty"`
	Message  string             `json:"message"`
}

type ValidationReport struct {
	Complete             bool                  `json:"complete"`
	RequiresConfirmation bool                  `json:"requiresConfirmation"`
	Violations           []ValidationViolation `json:"violations"`
}

// CanPublish reports whether this validation result may advance the published
// pointer. Incomplete collections, errors, and fatals all block publication.
func (r ValidationReport) CanPublish() bool {
	if !r.Complete {
		return false
	}
	for _, violation := range r.Violations {
		switch violation.Severity {
		case SeverityError, SeverityFatal:
			return false
		}
	}
	return !r.RequiresConfirmation
}

func (r *ValidationReport) Add(
	code string,
	severity ValidationSeverity,
	path string,
	message string,
) {
	r.Violations = append(r.Violations, ValidationViolation{
		Code:     code,
		Severity: severity,
		Path:     path,
		Message:  message,
	})
}

// SnapshotManifest proves that a candidate is complete: every expected page
// was fetched, every parsed record is unique, and the parsed count matches
// what the upstream reported.
type SnapshotManifest struct {
	SourceReportedCount int      `json:"sourceReportedCount"`
	ParsedCount         int      `json:"parsedCount"`
	UniqueCount         int      `json:"uniqueCount"`
	ExpectedPageCount   int      `json:"expectedPageCount"`
	FetchedPageCount    int      `json:"fetchedPageCount"`
	FirstRecordKey      string   `json:"firstRecordKey,omitempty"`
	LastRecordKey       string   `json:"lastRecordKey,omitempty"`
	Complete            bool     `json:"complete"`
	IncompleteReasons   []string `json:"incompleteReasons,omitempty"`
}

type ScrapeCandidate struct {
	TargetID              int64
	LeaseGeneration       int64
	AttemptNumber         int
	FetchedAt             time.Time
	RequestID             string
	HTTPStatus            int
	ContentType           string
	ContentLength         int64
	ETag                  string
	LastModified          string
	RawBodyHash           string
	CanonicalHash         string
	ParserVersion         string
	SchemaVersion         string
	CanonicalizerVersion  string
	Payload               json.RawMessage
	Manifest              SnapshotManifest
	Validation            ValidationReport
	Disposition           CandidateDisposition
	RejectionCode         string
	ConfirmationGroupUUID string
}

type CandidateDisposition string

const (
	CandidateAccepted               CandidateDisposition = "accepted"
	CandidateUnchanged              CandidateDisposition = "unchanged"
	CandidateNeedsConfirmation      CandidateDisposition = "needs_confirmation"
	CandidateRejectedTransport      CandidateDisposition = "rejected_transport"
	CandidateRejectedAuthentication CandidateDisposition = "rejected_authentication"
	CandidateRejectedParse          CandidateDisposition = "rejected_parse"
	CandidateRejectedIncomplete     CandidateDisposition = "rejected_incomplete"
	CandidateRejectedSemantic       CandidateDisposition = "rejected_semantic"
	CandidateQuarantinedAnomaly     CandidateDisposition = "quarantined_anomaly"
)
