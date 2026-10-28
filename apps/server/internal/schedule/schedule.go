// Package schedule works out when a scheduled task runs next. It is pure: the
// times come in, the next one goes out, and nothing is read or stored.
package schedule

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	// The zone database in the binary: the image it ships in may have none.
	_ "time/tzdata"
)

// Kinds of schedule.
const (
	Once    = "once"
	Daily   = "daily"
	Weekly  = "weekly"
	Monthly = "monthly"
)

// Spec is a schedule. A one-off runs at At; the others at Minute (minutes after
// midnight) in Zone - every day, on Weekdays (0 = Sunday), or on MonthDay (a
// month too short for it runs on its last day).
type Spec struct {
	Kind     string
	At       time.Time
	Minute   int
	Weekdays []int
	MonthDay int
	Zone     string
}

var (
	ErrKind     = errors.New("schedule: kind must be once, daily, weekly or monthly")
	ErrZone     = errors.New("schedule: unknown time zone")
	ErrMinute   = errors.New("schedule: time of day out of range")
	ErrWeekdays = errors.New("schedule: a weekly schedule needs at least one weekday, 0 (Sunday) to 6")
	ErrMonthDay = errors.New("schedule: day of month must be 1 to 31")
	ErrAt       = errors.New("schedule: a one-off needs a time")
)

// Validate checks a spec and puts it in order: weekdays sorted, once each.
func (s *Spec) Validate() error {
	if _, err := s.location(); err != nil {
		return err
	}
	switch s.Kind {
	case Once:
		if s.At.IsZero() {
			return ErrAt
		}
		return nil
	case Daily, Weekly, Monthly:
	default:
		return ErrKind
	}
	if s.Minute < 0 || s.Minute > 1439 {
		return ErrMinute
	}
	switch s.Kind {
	case Weekly:
		days := slices.Clone(s.Weekdays)
		slices.Sort(days)
		days = slices.Compact(days)
		if len(days) == 0 || days[0] < 0 || days[len(days)-1] > 6 {
			return ErrWeekdays
		}
		s.Weekdays = days
	case Monthly:
		if s.MonthDay < 1 || s.MonthDay > 31 {
			return ErrMonthDay
		}
	}
	return nil
}

func (s Spec) location() (*time.Location, error) {
	if strings.TrimSpace(s.Zone) == "" {
		return nil, ErrZone
	}
	loc, err := time.LoadLocation(s.Zone)
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrZone, s.Zone)
	}
	return loc, nil
}

// Next is the first time strictly after `after` the schedule names, or false
// when there is none: a one-off whose time has passed.
func (s Spec) Next(after time.Time) (time.Time, bool) {
	loc, err := s.location()
	if err != nil {
		return time.Time{}, false
	}
	if s.Kind == Once {
		if s.At.After(after) {
			return s.At, true
		}
		return time.Time{}, false
	}

	local := after.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	// A year of days is more than any of these needs; the bound only keeps a
	// spec that could never match (none can, once validated) from looping.
	for i := 0; i < 400; i++ {
		date := day.AddDate(0, 0, i)
		if !s.onDay(date) {
			continue
		}
		at := time.Date(date.Year(), date.Month(), date.Day(), s.Minute/60, s.Minute%60, 0, 0, loc)
		if at.After(after) {
			return at, true
		}
	}
	return time.Time{}, false
}

func (s Spec) onDay(date time.Time) bool {
	switch s.Kind {
	case Daily:
		return true
	case Weekly:
		return slices.Contains(s.Weekdays, int(date.Weekday()))
	case Monthly:
		last := time.Date(date.Year(), date.Month()+1, 0, 0, 0, 0, 0, date.Location()).Day()
		return date.Day() == min(s.MonthDay, last)
	}
	return false
}
