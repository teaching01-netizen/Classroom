package domain

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SnapshotKind string

const (
	SnapshotCourseCatalog   SnapshotKind = "course_catalog"
	SnapshotCourseDetail    SnapshotKind = "course_detail"
	SnapshotSessionDetail   SnapshotKind = "session_detail"
	SnapshotStudentProfiles SnapshotKind = "student_profiles"
)

func (k SnapshotKind) Valid() bool {
	switch k {
	case SnapshotCourseCatalog, SnapshotCourseDetail, SnapshotSessionDetail, SnapshotStudentProfiles:
		return true
	default:
		return false
	}
}

type TargetRef struct {
	Host        string
	Kind        SnapshotKind
	ResourceKey string
	ParentKey   string
}

func (r TargetRef) Validate() error {
	if strings.TrimSpace(r.Host) == "" {
		return errors.New("snapshot target host is required")
	}
	if !r.Kind.Valid() {
		return fmt.Errorf("invalid snapshot kind %q", r.Kind)
	}
	if strings.TrimSpace(r.ResourceKey) == "" {
		return errors.New("snapshot target resource key is required")
	}
	return nil
}

func (r TargetRef) IdentityKey() string {
	return string(r.Kind) + "\x00" + r.ParentKey + "\x00" + r.ResourceKey + "\x00" + r.Host
}

var (
	ErrSnapshotNotFound      = errors.New("snapshot not found")
	ErrSnapshotExpired       = errors.New("snapshot expired")
	ErrSnapshotRefreshFailed = errors.New("snapshot refresh failed")
	ErrNotModified           = errors.New("upstream not modified")
	ErrLeaseLost             = errors.New("scrape lease lost")
	ErrHostPaused            = errors.New("scrape host paused")
	ErrTargetLeased          = errors.New("scrape target already leased")
	ErrPermitUnavailable     = errors.New("host permit unavailable")
)

type ConditionalHeaders struct {
	ETag         string
	LastModified string
}

type ScrapeTarget struct {
	ID                  int64
	Ref                 TargetRef
	Attributes          json.RawMessage
	MissingCount        int
	CurrentContentHash  [32]byte
	HasCurrentSnapshot  bool
	CurrentVersion      int64
	ValidationSeq       int64
	CurrentInterval     time.Duration
	MinInterval         time.Duration
	MaxInterval         time.Duration
	MaxServeAge         time.Duration
	NextRunAt           time.Time
	LastValidatedAt     *time.Time
	ConsecutiveFailures int
	RecentChanges       []bool
	Conditional         ConditionalHeaders
	LeaseOwner          string
	LeaseGeneration     int64
	LeaseExpiresAt      *time.Time
}

func (t ScrapeTarget) HasContentHash() bool {
	return t.HasCurrentSnapshot && !IsZeroContentHash(t.CurrentContentHash)
}

type TargetSeed struct {
	Ref             TargetRef
	Attributes      json.RawMessage
	InitialInterval time.Duration
	MinInterval     time.Duration
	MaxInterval     time.Duration
	MaxServeAge     time.Duration
	NextRunAt       time.Time
}

type Snapshot struct {
	ID               int64           `json:"-"`
	TargetID         int64           `json:"-"`
	Ref              TargetRef       `json:"-"`
	Version          int64           `json:"version"`
	ValidationSeq    int64           `json:"validation_seq"`
	ContentHash      [32]byte        `json:"-"`
	Payload          json.RawMessage `json:"-"`
	ContentFetchedAt time.Time       `json:"content_fetched_at"`
	ValidatedAt      time.Time       `json:"validated_at"`
	NextRunAt        time.Time       `json:"next_run_at"`
	MaxServeAge      time.Duration   `json:"-"`
}

func (s Snapshot) Stale(now time.Time) bool {
	return now.After(s.NextRunAt)
}

func (s Snapshot) Expired(now time.Time) bool {
	if s.MaxServeAge <= 0 || s.ValidatedAt.IsZero() {
		return true
	}
	return now.Sub(s.ValidatedAt) > s.MaxServeAge
}

type SnapshotMetadata struct {
	Kind          SnapshotKind `json:"kind"`
	ResourceKey   string       `json:"resource_key"`
	ParentKey     string       `json:"parent_key"`
	Version       int64        `json:"version"`
	ValidationSeq int64        `json:"validation_seq"`
	ValidatedAt   time.Time    `json:"validated_at"`
	Stale         bool         `json:"stale"`
}

func (m SnapshotMetadata) Ref(host string) TargetRef {
	return TargetRef{
		Host:        host,
		Kind:        m.Kind,
		ResourceKey: m.ResourceKey,
		ParentKey:   m.ParentKey,
	}
}

type UpstreamStatusError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *UpstreamStatusError) Error() string {
	return fmt.Sprintf("upstream returned HTTP %d", e.StatusCode)
}

func IsZeroContentHash(hash [32]byte) bool {
	return hash == [32]byte{}
}

func ContentHashString(hash [32]byte) string {
	return hex.EncodeToString(hash[:])
}

type HostPermit struct {
	ID              int64
	Host            string
	TargetID        int64
	LeaseGeneration int64
	ExpiresAt       time.Time
}

type PermitDecision struct {
	Permit  *HostPermit
	RetryAt time.Time
	Paused  bool
}

type HostObservation struct {
	Host                string
	Outcome             string
	StatusCode          int
	RetryAfter          time.Duration
	Latency             time.Duration
	ConsecutiveFailures int
	ObservedAt          time.Time
}

type SessionFetcher interface {
	FetchSessionForReport(
		context.Context,
		string,
		string,
	) (*SessionDetail, error)
}
