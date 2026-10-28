package schedule

import (
	"errors"
	"testing"
	"time"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestNext(t *testing.T) {
	shanghai := mustZone(t, "Asia/Shanghai")
	newYork := mustZone(t, "America/New_York")
	at := func(loc *time.Location, y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, loc)
	}

	cases := []struct {
		name  string
		spec  Spec
		after time.Time
		want  time.Time
		none  bool
	}{
		{"daily later today", Spec{Kind: Daily, Minute: 9 * 60, Zone: "Asia/Shanghai"},
			at(shanghai, 2026, 10, 27, 8, 0), at(shanghai, 2026, 10, 27, 9, 0), false},
		{"daily at the minute itself is tomorrow", Spec{Kind: Daily, Minute: 9 * 60, Zone: "Asia/Shanghai"},
			at(shanghai, 2026, 10, 27, 9, 0), at(shanghai, 2026, 10, 28, 9, 0), false},
		{"weekly on the next listed day", Spec{Kind: Weekly, Minute: 18*60 + 30, Weekdays: []int{1, 5}, Zone: "Asia/Shanghai"},
			// 2026-10-27 is a Tuesday; Friday is the 30th.
			at(shanghai, 2026, 10, 27, 12, 0), at(shanghai, 2026, 10, 30, 18, 30), false},
		{"monthly on the 31st runs on a short month's last day", Spec{Kind: Monthly, Minute: 0, MonthDay: 31, Zone: "Asia/Shanghai"},
			at(shanghai, 2026, 11, 1, 0, 0), at(shanghai, 2026, 11, 30, 0, 0), false},
		{"monthly in February", Spec{Kind: Monthly, Minute: 60, MonthDay: 30, Zone: "Asia/Shanghai"},
			at(shanghai, 2027, 2, 1, 0, 0), at(shanghai, 2027, 2, 28, 1, 0), false},
		{"daily keeps the wall clock across a DST change", Spec{Kind: Daily, Minute: 9 * 60, Zone: "America/New_York"},
			// DST ends on 2026-11-01 in New York.
			at(newYork, 2026, 10, 31, 10, 0), at(newYork, 2026, 11, 1, 9, 0), false},
		{"a one-off still to come", Spec{Kind: Once, At: at(shanghai, 2026, 12, 1, 8, 0), Zone: "Asia/Shanghai"},
			at(shanghai, 2026, 10, 27, 0, 0), at(shanghai, 2026, 12, 1, 8, 0), false},
		{"a one-off that has passed", Spec{Kind: Once, At: at(shanghai, 2026, 10, 1, 8, 0), Zone: "Asia/Shanghai"},
			at(shanghai, 2026, 10, 27, 0, 0), time.Time{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := c.spec.Next(c.after)
			if c.none {
				if ok {
					t.Fatalf("Next = %v, want none", got)
				}
				return
			}
			if !ok || !got.Equal(c.want) {
				t.Fatalf("Next = %v (%v), want %v", got, ok, c.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	weekly := Spec{Kind: Weekly, Minute: 60, Weekdays: []int{5, 1, 5}, Zone: "UTC"}
	if err := weekly.Validate(); err != nil || len(weekly.Weekdays) != 2 || weekly.Weekdays[0] != 1 {
		t.Fatalf("weekly: %v %v", err, weekly.Weekdays)
	}
	for name, c := range map[string]struct {
		spec Spec
		want error
	}{
		"kind":       {Spec{Kind: "hourly", Zone: "UTC"}, ErrKind},
		"zone":       {Spec{Kind: Daily, Zone: "Mars/Olympus"}, ErrZone},
		"no zone":    {Spec{Kind: Daily}, ErrZone},
		"minute":     {Spec{Kind: Daily, Minute: 1440, Zone: "UTC"}, ErrMinute},
		"no days":    {Spec{Kind: Weekly, Zone: "UTC"}, ErrWeekdays},
		"bad day":    {Spec{Kind: Weekly, Weekdays: []int{7}, Zone: "UTC"}, ErrWeekdays},
		"month day":  {Spec{Kind: Monthly, MonthDay: 0, Zone: "UTC"}, ErrMonthDay},
		"once no at": {Spec{Kind: Once, Zone: "UTC"}, ErrAt},
	} {
		spec := c.spec
		if err := spec.Validate(); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}
