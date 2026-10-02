// Package bizprocess mirrors PRTG Business Process sensors and turns their
// state history into business impact: downtime, SLA position and estimated
// financial loss per business process.
package bizprocess

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	Up      = "up"
	Warning = "warning"
	Down    = "down"
	Unknown = "unknown"

	// GlobalChannel is PRTG's "Global State" channel: the process as a whole.
	GlobalChannel = 0
)

// StateFromValueRaw maps a Business Process channel's raw lookup value from
// PRTG historic data (0 Up, 100 Warning, 200 Down; -1 or empty = no data).
func StateFromValueRaw(v string) string {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return Unknown
	}
	switch f {
	case 0:
		return Up
	case 100:
		return Warning
	case 200:
		return Down
	}
	return Unknown
}

// StateFromText maps a channel's last value or a sensor status text
// ("Up", "Warning", "Down") to a state.
func StateFromText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(s, "down"):
		return Down
	case strings.HasPrefix(s, "warn"):
		return Warning
	case strings.HasPrefix(s, "up"), s == "ok":
		return Up
	}
	return Unknown
}

// MemberState classifies a member object's PRTG status and reports whether
// PRTG's Business Process logic counts it as "up". PRTG counts Up, Warning,
// Unusual, Down (Partial) and Unknown (Collecting) as up; Down,
// Down (Acknowledged), Paused and Unknown as down — so a paused member drags
// its channel down even though nothing is broken.
func MemberState(status string) (state string, countsAsUp bool) {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.HasPrefix(s, "paused"):
		return "paused", false
	case strings.HasPrefix(s, "down") && strings.Contains(s, "partial"):
		return Down, true
	case strings.HasPrefix(s, "down"):
		return Down, false
	case strings.HasPrefix(s, "warning"), strings.HasPrefix(s, "unusual"):
		return Warning, true
	case strings.HasPrefix(s, "up"):
		return Up, true
	case strings.Contains(s, "collecting"):
		return Unknown, true
	}
	return Unknown, false
}

// Interval is a stretch of time a channel spent in one state.
type Interval struct {
	ChannelID int
	State     string
	Start     time.Time
	End       time.Time
}

func (i Interval) Seconds() float64 { return i.End.Sub(i.Start).Seconds() }

// Sample is one observed channel state at a point in time.
type Sample struct {
	At    time.Time
	State string
}

// ExtendIntervals folds samples (sorted ascending, all after tail.End) into
// a channel's timeline. Like PRTG, a scan's state holds until the next scan;
// when scans are missing (gap over 1.5 scan intervals) the state holds for one
// scan interval and the rest of the gap is recorded as unknown rather than
// assumed. It returns the updated tail copy (nil if none was given) and the
// intervals created after it.
func ExtendIntervals(tail *Interval, channelID int, samples []Sample, scan time.Duration) (*Interval, []Interval) {
	maxGap := scan + scan/2
	var created []Interval
	var updatedTail *Interval
	if tail != nil {
		t := *tail
		updatedTail = &t
	}
	// cur is the interval currently being extended: the tail copy first, then
	// always the last element of created (re-taken after every append).
	cur := updatedTail
	open := func(state string, at time.Time) {
		created = append(created, Interval{ChannelID: channelID, State: state, Start: at, End: at})
		cur = &created[len(created)-1]
	}
	for _, s := range samples {
		if cur == nil {
			open(s.State, s.At)
			continue
		}
		if !s.At.After(cur.End) {
			continue
		}
		if s.At.Sub(cur.End) > maxGap {
			cur.End = cur.End.Add(scan)
			open(Unknown, cur.End)
			cur.End = s.At
			open(s.State, s.At)
			continue
		}
		cur.End = s.At
		if s.State != cur.State {
			open(s.State, s.At)
		}
	}
	return updatedTail, created
}

// Clip returns the intervals overlapping [from, to], trimmed to that window.
func Clip(intervals []Interval, from, to time.Time) []Interval {
	out := []Interval{}
	for _, iv := range intervals {
		if !iv.End.After(from) || !iv.Start.Before(to) {
			continue
		}
		if iv.Start.Before(from) {
			iv.Start = from
		}
		if iv.End.After(to) {
			iv.End = to
		}
		out = append(out, iv)
	}
	return out
}

// ExtendToNow stretches the newest interval to now when the last observation
// is recent (within maxStale), so an ongoing outage keeps accruing between
// syncs without assuming a state for data that is too old.
func ExtendToNow(intervals []Interval, now time.Time, maxStale time.Duration) []Interval {
	if len(intervals) == 0 {
		return intervals
	}
	out := append([]Interval(nil), intervals...)
	last := &out[len(out)-1]
	if now.After(last.End) && now.Sub(last.End) <= maxStale {
		last.End = now
	}
	return out
}

// Rates are the loss rates of one business process while fully down.
type Rates struct {
	RevenuePerHour      float64 // hourly_revenue × service_dependency × loss_probability
	ProductivityPerHour float64 // affected_employees × avg_employee_cost_per_hour
	OperationalPerHour  float64 // operational_cost_per_hour
	DegradedFactor      float64 // share of the hourly loss that applies while degraded (0..1)
	RecoveryFixed       float64 // per outage that went down
	PenaltyFixed        float64 // per outage whose downtime exceeded the RTO
	RTOSeconds          float64
}

func (r Rates) FullPerHour() float64 {
	return r.RevenuePerHour + r.ProductivityPerHour + r.OperationalPerHour
}

type Loss struct {
	Revenue      float64 `json:"revenue"`
	Productivity float64 `json:"productivity"`
	Operational  float64 `json:"operational"`
	Recovery     float64 `json:"recovery"`
	Penalty      float64 `json:"penalty"`
	Total        float64 `json:"total"`
}

func (l *Loss) add(o Loss) {
	l.Revenue += o.Revenue
	l.Productivity += o.Productivity
	l.Operational += o.Operational
	l.Recovery += o.Recovery
	l.Penalty += o.Penalty
	l.Total += o.Total
}

// LossFor estimates the loss of one outage episode. Degraded time costs
// DegradedFactor of the hourly rate; the recovery cost applies once if the
// process went down, and the penalty once if downtime exceeded the RTO.
func (r Rates) LossFor(downSeconds, degradedSeconds float64) Loss {
	hours := downSeconds/3600 + degradedSeconds/3600*r.DegradedFactor
	l := Loss{
		Revenue:      r.RevenuePerHour * hours,
		Productivity: r.ProductivityPerHour * hours,
		Operational:  r.OperationalPerHour * hours,
	}
	if downSeconds > 0 {
		l.Recovery = r.RecoveryFixed
	}
	if r.RTOSeconds > 0 && downSeconds > r.RTOSeconds {
		l.Penalty = r.PenaltyFixed
	}
	l.Total = l.Revenue + l.Productivity + l.Operational + l.Recovery + l.Penalty
	return l
}

// Cause is a component channel's share of an episode.
type Cause struct {
	ChannelID       int     `json:"channel_id"`
	DownSeconds     float64 `json:"down_seconds"`
	DegradedSeconds float64 `json:"degraded_seconds"`
}

// Episode is one continuous period the process was not fully up.
type Episode struct {
	Start           time.Time
	End             time.Time
	Ongoing         bool
	DownSeconds     float64
	DegradedSeconds float64
	Worst           string
	Causes          []Cause
	Loss            Loss
}

func (e Episode) Seconds() float64 { return e.End.Sub(e.Start).Seconds() }

// BuildEpisodes groups consecutive Warning/Down global intervals into
// episodes and attributes them to the component channels that were not up
// at the same time. Intervals must be sorted and already clipped.
func BuildEpisodes(global []Interval, channels map[int][]Interval, rates Rates, ongoing bool) []Episode {
	var eps []Episode
	var cur *Episode
	flush := func() {
		if cur != nil {
			eps = append(eps, *cur)
			cur = nil
		}
	}
	for _, iv := range global {
		bad := iv.State == Down || iv.State == Warning
		if !bad || (cur != nil && !iv.Start.Equal(cur.End)) {
			flush()
		}
		if !bad {
			continue
		}
		if cur == nil {
			cur = &Episode{Start: iv.Start, End: iv.Start, Worst: iv.State}
		}
		cur.End = iv.End
		if iv.State == Down {
			cur.DownSeconds += iv.Seconds()
			cur.Worst = Down
		} else {
			cur.DegradedSeconds += iv.Seconds()
		}
	}
	flush()
	if ongoing && len(eps) > 0 && len(global) > 0 && eps[len(eps)-1].End.Equal(global[len(global)-1].End) {
		eps[len(eps)-1].Ongoing = true
	}
	ids := make([]int, 0, len(channels))
	for id := range channels {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for i := range eps {
		e := &eps[i]
		for _, id := range ids {
			c := Cause{ChannelID: id}
			for _, iv := range Clip(channels[id], e.Start, e.End) {
				switch iv.State {
				case Down:
					c.DownSeconds += iv.Seconds()
				case Warning:
					c.DegradedSeconds += iv.Seconds()
				}
			}
			if c.DownSeconds > 0 || c.DegradedSeconds > 0 {
				e.Causes = append(e.Causes, c)
			}
		}
		e.Loss = rates.LossFor(e.DownSeconds, e.DegradedSeconds)
	}
	return eps
}

// Stats summarizes a period. Availability follows PRTG's uptime definition:
// Warning counts as available (degraded), only Down counts against it, and
// time without data is excluded.
type Stats struct {
	UpSeconds       float64 `json:"up_seconds"`
	WarningSeconds  float64 `json:"warning_seconds"`
	DownSeconds     float64 `json:"down_seconds"`
	UnknownSeconds  float64 `json:"unknown_seconds"`
	AvailabilityPct float64 `json:"availability_pct"`
	DegradedPct     float64 `json:"degraded_pct"`
	Outages         int     `json:"outages"`
	Degradations    int     `json:"degradations"`
	MTTRSeconds     float64 `json:"mttr_seconds"`
	LongestSeconds  float64 `json:"longest_seconds"`
	Loss            Loss    `json:"loss"`
}

func Summarize(global []Interval, eps []Episode) Stats {
	var s Stats
	for _, iv := range global {
		switch iv.State {
		case Up:
			s.UpSeconds += iv.Seconds()
		case Warning:
			s.WarningSeconds += iv.Seconds()
		case Down:
			s.DownSeconds += iv.Seconds()
		default:
			s.UnknownSeconds += iv.Seconds()
		}
	}
	known := s.UpSeconds + s.WarningSeconds + s.DownSeconds
	if known > 0 {
		s.AvailabilityPct = (s.UpSeconds + s.WarningSeconds) / known * 100
		s.DegradedPct = s.WarningSeconds / known * 100
	}
	var restored float64
	var restoredN int
	for _, e := range eps {
		if e.DownSeconds > 0 {
			s.Outages++
			if !e.Ongoing {
				restored += e.Seconds()
				restoredN++
			}
		} else {
			s.Degradations++
		}
		if e.Seconds() > s.LongestSeconds {
			s.LongestSeconds = e.Seconds()
		}
		s.Loss.add(e.Loss)
	}
	if restoredN > 0 {
		s.MTTRSeconds = restored / float64(restoredN)
	}
	return s
}

// ChannelSeconds returns how long a component channel was down/degraded.
func ChannelSeconds(intervals []Interval) (down, degraded float64) {
	for _, iv := range intervals {
		switch iv.State {
		case Down:
			down += iv.Seconds()
		case Warning:
			degraded += iv.Seconds()
		}
	}
	return down, degraded
}
