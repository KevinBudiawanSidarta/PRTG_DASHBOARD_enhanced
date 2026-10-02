package bizprocess

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func TestStateFromValueRaw(t *testing.T) {
	cases := map[string]string{"0": Up, "100": Warning, "200": Down, "-1": Unknown, "": Unknown, "50": Unknown}
	for in, want := range cases {
		if got := StateFromValueRaw(in); got != want {
			t.Errorf("StateFromValueRaw(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMemberStateFollowsPRTGBusinessProcessRules(t *testing.T) {
	cases := []struct {
		status string
		state  string
		up     bool
	}{
		{"Up", Up, true},
		{"Warning", Warning, true},
		{"Unusual", Warning, true},
		{"Down (Partial)", Down, true},
		{"Down", Down, false},
		{"Down (Acknowledged)", Down, false},
		{"Paused  (paused by parent)", "paused", false},
		{"Unknown", Unknown, false},
	}
	for _, c := range cases {
		state, up := MemberState(c.status)
		if state != c.state || up != c.up {
			t.Errorf("MemberState(%q) = %q,%v want %q,%v", c.status, state, up, c.state, c.up)
		}
	}
}

func TestExtendIntervalsMergesAndSplitsOnStateChange(t *testing.T) {
	samples := []Sample{{at(0), Up}, {at(1), Up}, {at(2), Down}, {at(3), Down}, {at(4), Up}}
	tail, created := ExtendIntervals(nil, 0, samples, time.Minute)
	if tail != nil {
		t.Fatal("expected no tail when none was given")
	}
	want := []Interval{{0, Up, at(0), at(2)}, {0, Down, at(2), at(4)}, {0, Up, at(4), at(4)}}
	if len(created) != len(want) {
		t.Fatalf("got %d intervals %+v, want %d", len(created), created, len(want))
	}
	for i := range want {
		if created[i] != want[i] {
			t.Errorf("interval %d = %+v, want %+v", i, created[i], want[i])
		}
	}
}

func TestExtendIntervalsContinuesTailAndSkipsOldSamples(t *testing.T) {
	tail := &Interval{0, Down, at(0), at(5)}
	tail2, created := ExtendIntervals(tail, 0, []Sample{{at(4), Up}, {at(6), Down}, {at(7), Down}}, time.Minute)
	if len(created) != 0 {
		t.Fatalf("expected the tail to be extended, got new intervals %+v", created)
	}
	if !tail2.End.Equal(at(7)) || !tail.End.Equal(at(5)) {
		t.Fatalf("tail end = %v (original %v), want extended copy to %v", tail2.End, tail.End, at(7))
	}
}

func TestExtendIntervalsRecordsGapsAsUnknown(t *testing.T) {
	tail := &Interval{0, Down, at(0), at(1)}
	tail2, created := ExtendIntervals(tail, 0, []Sample{{at(30), Up}}, time.Minute)
	// The last scan's state covers one scan interval (1→2), the rest is unknown.
	if !tail2.End.Equal(at(2)) {
		t.Fatalf("tail should hold one scan interval, ends at %v", tail2.End)
	}
	if len(created) != 2 || created[0].State != Unknown || !created[0].Start.Equal(at(2)) || !created[0].End.Equal(at(30)) || created[1].State != Up {
		t.Fatalf("expected an unknown gap 2→30 then a new up interval, got %+v", created)
	}
}

func TestExtendIntervalsToleratesScanJitter(t *testing.T) {
	jitter := time.Duration(80) * time.Second // under 1.5 × 60s
	_, created := ExtendIntervals(nil, 0, []Sample{{t0, Down}, {t0.Add(jitter), Down}}, time.Minute)
	if len(created) != 1 || created[0].Seconds() != 80 {
		t.Fatalf("expected one 80s down interval, got %+v", created)
	}
}

func TestEpisodesLossAndCauses(t *testing.T) {
	global := []Interval{
		{0, Up, at(0), at(10)},
		{0, Warning, at(10), at(40)},
		{0, Down, at(40), at(100)},
		{0, Up, at(100), at(120)},
	}
	channels := map[int][]Interval{
		1: {{1, Up, at(0), at(120)}},
		2: {{2, Up, at(0), at(10)}, {2, Down, at(10), at(100)}, {2, Up, at(100), at(120)}},
	}
	rates := Rates{RevenuePerHour: 1000, ProductivityPerHour: 200, OperationalPerHour: 100, DegradedFactor: 0.5, RecoveryFixed: 50, PenaltyFixed: 500, RTOSeconds: 30 * 60}
	eps := BuildEpisodes(global, channels, rates, false)
	if len(eps) != 1 {
		t.Fatalf("expected one merged episode, got %d", len(eps))
	}
	e := eps[0]
	if e.Worst != Down || e.DownSeconds != 3600 || e.DegradedSeconds != 1800 {
		t.Fatalf("unexpected episode %+v", e)
	}
	if len(e.Causes) != 1 || e.Causes[0].ChannelID != 2 || e.Causes[0].DownSeconds != 90*60 {
		t.Fatalf("expected channel 2 as the only cause, got %+v", e.Causes)
	}
	// 1h down + 0.5h degraded × 0.5 = 1.25 effective hours × 1300/h, plus
	// recovery (went down) and penalty (60 min down > 30 min RTO).
	want := 1.25*1300 + 50 + 500
	if math.Abs(e.Loss.Total-want) > 1e-6 {
		t.Fatalf("loss total = %v, want %v", e.Loss.Total, want)
	}

	s := Summarize(global, eps)
	if s.Outages != 1 || math.Abs(s.AvailabilityPct-50) > 1e-9 || s.MTTRSeconds != 90*60 {
		t.Fatalf("unexpected stats %+v", s)
	}
}

func TestDegradedOnlyEpisodeHasNoRecoveryOrPenalty(t *testing.T) {
	rates := Rates{RevenuePerHour: 1000, DegradedFactor: 0.3, RecoveryFixed: 50, PenaltyFixed: 500, RTOSeconds: 60}
	l := rates.LossFor(0, 7200)
	if l.Recovery != 0 || l.Penalty != 0 || math.Abs(l.Total-600) > 1e-9 {
		t.Fatalf("unexpected degraded-only loss %+v", l)
	}
}

func TestClipAndExtendToNow(t *testing.T) {
	ivs := []Interval{{0, Up, at(0), at(10)}, {0, Down, at(10), at(20)}}
	c := Clip(ivs, at(5), at(15))
	if len(c) != 2 || !c[0].Start.Equal(at(5)) || !c[1].End.Equal(at(15)) {
		t.Fatalf("unexpected clip %+v", c)
	}
	if got := ExtendToNow(ivs, at(25), 10*time.Minute); !got[1].End.Equal(at(25)) || !ivs[1].End.Equal(at(20)) {
		t.Fatal("expected a recent tail to be extended to now on a copy")
	}
	if got := ExtendToNow(ivs, at(90), 10*time.Minute); !got[1].End.Equal(at(20)) {
		t.Fatal("expected a stale tail not to be extended")
	}
}
