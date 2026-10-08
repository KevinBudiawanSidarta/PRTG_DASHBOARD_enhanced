package jetdata

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Update is a partial update of one existing row (changed fields only).
type Update struct {
	ID     int
	Values map[string]string
}

// Plan is the set of writes that makes a JET form match the desired rows.
type Plan struct {
	Create []map[string]string
	Update []Update
	Delete []int
}

func (p Plan) Empty() bool { return len(p.Create)+len(p.Update)+len(p.Delete) == 0 }

// Diff matches existing rows to desired rows on keyField. Rows whose key is
// not desired are deleted, as are duplicate rows for the same key (the
// oldest is kept), so the form self-heals from double writes. Only fields
// whose value actually changed are sent in an update.
func Diff(existing []Record, desired []map[string]string, keyField string) Plan {
	var plan Plan
	sorted := append([]Record(nil), existing...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	byKey := map[string]Record{}
	for _, r := range sorted {
		k := strings.TrimSpace(r.Values[keyField])
		if _, dup := byKey[k]; dup || k == "" {
			plan.Delete = append(plan.Delete, r.ID)
			continue
		}
		byKey[k] = r
	}
	wanted := map[string]bool{}
	for _, d := range desired {
		k := strings.TrimSpace(d[keyField])
		if k == "" || wanted[k] {
			continue
		}
		wanted[k] = true
		r, ok := byKey[k]
		if !ok {
			plan.Create = append(plan.Create, d)
			continue
		}
		changed := map[string]string{}
		for f, v := range d {
			if !SameValue(r.Values[f], v) {
				changed[f] = v
			}
		}
		if len(changed) > 0 {
			plan.Update = append(plan.Update, Update{ID: r.ID, Values: changed})
		}
	}
	for k, r := range byKey {
		if !wanted[k] {
			plan.Delete = append(plan.Delete, r.ID)
		}
	}
	sort.Ints(plan.Delete)
	return plan
}

// SameValue compares stored and desired values, treating numbers that only
// differ in formatting ("12.5" vs "12.50") as equal.
func SameValue(stored, desired string) bool {
	a, b := strings.TrimSpace(stored), strings.TrimSpace(desired)
	if a == b {
		return true
	}
	fa, errA := strconv.ParseFloat(a, 64)
	fb, errB := strconv.ParseFloat(b, 64)
	return errA == nil && errB == nil && math.Abs(fa-fb) < 0.005
}
