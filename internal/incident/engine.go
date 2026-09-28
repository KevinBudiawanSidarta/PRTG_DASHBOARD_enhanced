package incident

import "time"

type Status string

const (
	Open         Status = "OPEN"
	Acknowledged Status = "ACKNOWLEDGED"
	Resolved     Status = "RESOLVED"
	Closed       Status = "CLOSED"
)

type Severity string

const (
	Info     Severity = "INFO"
	Warning  Severity = "WARNING"
	Minor    Severity = "MINOR"
	Major    Severity = "MAJOR"
	Critical Severity = "CRITICAL"
)

type State struct {
	Status   Status
	Severity Severity
}

func CanTransition(from, to Status) bool {
	switch from {
	case Open:
		return to == Acknowledged || to == Resolved || to == Closed
	case Acknowledged:
		return to == Resolved || to == Closed
	case Resolved:
		return to == Closed
	case Closed:
		return false
	}
	return false
}
func SeverityFor(dependency, durationSeconds float64) Severity {
	// MVP severity heuristic; exact rules are not specified in the architecture document.
	if dependency >= .8 && durationSeconds >= 900 {
		return Critical
	}
	if dependency >= .6 && durationSeconds >= 600 {
		return Major
	}
	if durationSeconds >= 300 {
		return Minor
	}
	if durationSeconds > 0 {
		return Warning
	}
	return Info
}
func Duration(start, end time.Time) int64 {
	if end.IsZero() {
		end = time.Now().UTC()
	}
	if end.Before(start) {
		return 0
	}
	return int64(end.Sub(start).Seconds())
}

// CorrelationWindow is how close together two incidents must start to be
// treated as a likely shared-root-cause cluster (e.g. one upstream device
// failing takes multiple dependent sensors down within the same window).
// This is a temporal heuristic only - the platform has no real network
// topology graph, so proximity in time is the signal used instead of a
// true dependency chain.
const CorrelationWindow = 3 * time.Minute

// Correlates reports whether two incidents starting at a and b are close
// enough in time to likely share a root cause.
func Correlates(a, b time.Time) bool {
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d <= CorrelationWindow
}
