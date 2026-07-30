package scraper

import (
	"math"
	"math/rand"
	"time"

	"qr-command-center/internal/domain"
)

type Outcome int

const (
	OutcomeChanged Outcome = iota
	OutcomeUnchanged
	OutcomeNotModified
)

type Policy struct {
	Initial     time.Duration
	Min         time.Duration
	Max         time.Duration
	MaxServeAge time.Duration
}

type Schedule struct {
	Interval time.Duration
	History  []bool
}

func PolicyFor(kind domain.SnapshotKind, status domain.SessionStatus) Policy {
	switch kind {
	case domain.SnapshotCourseCatalog, domain.SnapshotCourseDetail:
		return Policy{
			Initial:     time.Hour,
			Min:         15 * time.Minute,
			Max:         24 * time.Hour,
			MaxServeAge: 48 * time.Hour,
		}
	case domain.SnapshotSessionDetail:
		switch status {
		case domain.SessionStatusDone:
			return Policy{
				Initial:     12 * time.Hour,
				Min:         time.Hour,
				Max:         30 * 24 * time.Hour,
				MaxServeAge: 45 * 24 * time.Hour,
			}
		case domain.SessionStatusNotStarted:
			return Policy{
				Initial:     time.Hour,
				Min:         15 * time.Minute,
				Max:         12 * time.Hour,
				MaxServeAge: 24 * time.Hour,
			}
		default:
			return Policy{
				Initial:     5 * time.Minute,
				Min:         time.Minute,
				Max:         30 * time.Minute,
				MaxServeAge: 2 * time.Hour,
			}
		}
	case domain.SnapshotStudentProfiles:
		return Policy{
			Initial:     24 * time.Hour,
			Min:         6 * time.Hour,
			Max:         7 * 24 * time.Hour,
			MaxServeAge: 14 * 24 * time.Hour,
		}
	default:
		return Policy{}
	}
}

func NextSchedule(policy Policy, current time.Duration, history []bool, outcome Outcome) Schedule {
	if current <= 0 {
		current = policy.Initial
	}
	if current <= 0 {
		current = policy.Min
	}

	changed := outcome == OutcomeChanged
	nextHistory := append(append([]bool(nil), history...), changed)
	if len(nextHistory) > 10 {
		nextHistory = append([]bool(nil), nextHistory[len(nextHistory)-10:]...)
	}

	interval := current
	if changed {
		interval = current / 2
	} else if len(nextHistory) == 10 && allUnchanged(nextHistory) {
		interval = current * 2
	} else {
		interval = time.Duration(math.Round(float64(current) * 1.5))
	}

	if policy.Min > 0 && interval < policy.Min {
		interval = policy.Min
	}
	if policy.Max > 0 && interval > policy.Max {
		interval = policy.Max
	}
	return Schedule{Interval: interval, History: nextHistory}
}

func allUnchanged(history []bool) bool {
	for _, changed := range history {
		if changed {
			return false
		}
	}
	return true
}

func FailureDelay(consecutiveFailures int) time.Duration {
	if consecutiveFailures <= 1 {
		return time.Minute
	}
	if consecutiveFailures > 7 {
		return time.Hour
	}
	delay := time.Minute * time.Duration(1<<uint(consecutiveFailures-1))
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func HostPauseFor429(now time.Time, last429 *time.Time, retryAfter time.Duration) time.Duration {
	pause := 15 * time.Minute
	if retryAfter > pause {
		pause = retryAfter
	}
	if last429 != nil && !last429.IsZero() && now.Sub(*last429) <= time.Hour {
		if pause < time.Hour {
			pause = time.Hour
		}
	}
	return pause
}

func ApplyJitter(base time.Duration, fraction float64, rng *rand.Rand) time.Duration {
	if base <= 0 || fraction <= 0 || rng == nil {
		return base
	}
	if fraction > 1 {
		fraction = 1
	}
	offset := (rng.Float64()*2 - 1) * fraction * float64(base)
	result := time.Duration(math.Round(float64(base) + offset))
	if result < 0 {
		return 0
	}
	return result
}

func FullJitter(cap time.Duration, rng *rand.Rand) time.Duration {
	if cap <= 0 || rng == nil {
		return 0
	}
	return time.Duration(rng.Int63n(int64(cap) + 1))
}

// LateCorrectionPolicy returns a Policy suitable for sessions that completed
// long ago and whose scraping interval may have expanded beyond their
// correction window. Newer sessions get tighter intervals; older sessions
// get progressively coarser corrections.
func LateCorrectionPolicy(sessionCompletedAt time.Time, now time.Time) Policy {
	age := now.Sub(sessionCompletedAt)
	switch {
	case age < 24*time.Hour:
		return Policy{Initial: 5 * time.Minute, Min: 5 * time.Minute, Max: 30 * time.Minute}
	case age < 7*24*time.Hour:
		return Policy{Initial: 1 * time.Hour, Min: 1 * time.Hour, Max: 6 * time.Hour}
	case age < 30*24*time.Hour:
		return Policy{Initial: 24 * time.Hour, Min: 24 * time.Hour, Max: 24 * time.Hour}
	default:
		return Policy{Initial: 7 * 24 * time.Hour, Min: 7 * 24 * time.Hour, Max: 7 * 24 * time.Hour}
	}
}
