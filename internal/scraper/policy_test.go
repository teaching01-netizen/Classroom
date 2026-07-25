package scraper

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestNextSchedule(t *testing.T) {
	policy := Policy{
		Initial:     5 * time.Minute,
		Min:         time.Minute,
		Max:         30 * time.Minute,
		MaxServeAge: 2 * time.Hour,
	}

	tests := []struct {
		name        string
		current     time.Duration
		history     []bool
		outcome     Outcome
		want        time.Duration
		wantHistory []bool
	}{
		{"changed halves", 10 * time.Minute, nil, OutcomeChanged, 5 * time.Minute, []bool{true}},
		{"changed minimum", time.Minute, nil, OutcomeChanged, time.Minute, []bool{true}},
		{"unchanged adds half", 10 * time.Minute, []bool{true}, OutcomeUnchanged, 15 * time.Minute, []bool{true, false}},
		{"not modified is unchanged", 10 * time.Minute, []bool{true}, OutcomeNotModified, 15 * time.Minute, []bool{true, false}},
		{"ten unchanged doubles", 10 * time.Minute, make([]bool, 9), OutcomeUnchanged, 20 * time.Minute, make([]bool, 10)},
		{"maximum", 30 * time.Minute, nil, OutcomeUnchanged, 30 * time.Minute, []bool{false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := append([]bool(nil), tt.history...)
			got := NextSchedule(policy, tt.current, input, tt.outcome)
			require.Equal(t, tt.want, got.Interval)
			require.Equal(t, tt.wantHistory, got.History)
			require.Equal(t, tt.history, input, "input history must not be mutated")
		})
	}
}

func TestNextScheduleTrimsHistoryToLatestTen(t *testing.T) {
	history := []bool{true, true, false, true, false, true, false, true, false, true}
	got := NextSchedule(Policy{Min: time.Minute, Max: time.Hour}, 10*time.Minute, history, OutcomeUnchanged)
	require.Len(t, got.History, 10)
	require.Equal(t, append(append([]bool(nil), history[1:]...), false), got.History)
}

func TestApplyJitterIsDeterministicAndBounded(t *testing.T) {
	first := ApplyJitter(10*time.Minute, 0.1, rand.New(rand.NewSource(17)))
	second := ApplyJitter(10*time.Minute, 0.1, rand.New(rand.NewSource(17)))
	require.Equal(t, first, second)
	require.GreaterOrEqual(t, first, 9*time.Minute)
	require.LessOrEqual(t, first, 11*time.Minute)
}

func TestFailureDelay(t *testing.T) {
	require.Equal(t, time.Minute, FailureDelay(1))
	require.Equal(t, 2*time.Minute, FailureDelay(2))
	require.Equal(t, 4*time.Minute, FailureDelay(3))
	require.Equal(t, time.Hour, FailureDelay(99))
}

func TestHostPauseFor429(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	require.Equal(t, 20*time.Minute, HostPauseFor429(now, nil, 20*time.Minute))
	require.Equal(t, 15*time.Minute, HostPauseFor429(now, nil, 0))
	recent := now.Add(-30 * time.Minute)
	require.Equal(t, time.Hour, HostPauseFor429(now, &recent, 20*time.Minute))
	require.Equal(t, 90*time.Minute, HostPauseFor429(now, &recent, 90*time.Minute))
}

func TestPolicyForKindsAndSessionStates(t *testing.T) {
	tests := []struct {
		name   string
		kind   domain.SnapshotKind
		status domain.SessionStatus
		want   Policy
	}{
		{"catalog", domain.SnapshotCourseCatalog, "", Policy{time.Hour, 15 * time.Minute, 24 * time.Hour, 48 * time.Hour}},
		{"course", domain.SnapshotCourseDetail, "", Policy{time.Hour, 15 * time.Minute, 24 * time.Hour, 48 * time.Hour}},
		{"active session", domain.SnapshotSessionDetail, domain.SessionStatusActive, Policy{5 * time.Minute, time.Minute, 30 * time.Minute, 2 * time.Hour}},
		{"not started session", domain.SnapshotSessionDetail, domain.SessionStatusNotStarted, Policy{time.Hour, 15 * time.Minute, 12 * time.Hour, 24 * time.Hour}},
		{"finished session", domain.SnapshotSessionDetail, domain.SessionStatusDone, Policy{12 * time.Hour, time.Hour, 30 * 24 * time.Hour, 45 * 24 * time.Hour}},
		{"profiles", domain.SnapshotStudentProfiles, "", Policy{24 * time.Hour, 6 * time.Hour, 7 * 24 * time.Hour, 14 * 24 * time.Hour}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, PolicyFor(tt.kind, tt.status))
		})
	}
}
