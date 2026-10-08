package jetdata

import (
	"reflect"
	"testing"
)

func rec(id int, kv ...string) Record {
	r := Record{ID: id, Values: map[string]string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		r.Values[kv[i]] = kv[i+1]
	}
	return r
}

func TestDiffCreatesUpdatesAndDeletes(t *testing.T) {
	existing := []Record{
		rec(1, "Key", "a", "Status", "up", "Value", "12.50"),
		rec(2, "Key", "b", "Status", "up"),
		rec(3, "Key", "gone", "Status", "down"),
	}
	desired := []map[string]string{
		{"Key": "a", "Status": "up", "Value": "12.5"}, // unchanged: numeric formatting only
		{"Key": "b", "Status": "down"},                // changed
		{"Key": "c", "Status": "up"},                  // new
	}
	p := Diff(existing, desired, "Key")
	if len(p.Create) != 1 || p.Create[0]["Key"] != "c" {
		t.Fatalf("create = %+v", p.Create)
	}
	if len(p.Update) != 1 || p.Update[0].ID != 2 || !reflect.DeepEqual(p.Update[0].Values, map[string]string{"Status": "down"}) {
		t.Fatalf("update = %+v", p.Update)
	}
	if !reflect.DeepEqual(p.Delete, []int{3}) {
		t.Fatalf("delete = %v", p.Delete)
	}
}

func TestDiffRemovesDuplicatesKeepingOldest(t *testing.T) {
	existing := []Record{rec(7, "Key", "a"), rec(5, "Key", "a"), rec(9, "Key", "a"), rec(10, "Key", "")}
	p := Diff(existing, []map[string]string{{"Key": "a"}}, "Key")
	if len(p.Create) != 0 || len(p.Update) != 0 || !reflect.DeepEqual(p.Delete, []int{7, 9, 10}) {
		t.Fatalf("unexpected plan %+v", p)
	}
}

func TestDiffNoChangesIsEmpty(t *testing.T) {
	p := Diff([]Record{rec(1, "Key", "a", "N", "3")}, []map[string]string{{"Key": "a", "N": "3.000"}}, "Key")
	if !p.Empty() {
		t.Fatalf("expected empty plan, got %+v", p)
	}
}
