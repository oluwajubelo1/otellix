package otellix

import (
	"fmt"
	"time"
)

type windowKind int

const (
	windowRolling windowKind = iota
	windowCalendarDay
	windowCalendarMonth
)

// DefaultBudgetInterval is the rolling window used when none is configured.
const DefaultBudgetInterval = 24 * time.Hour

// BudgetWindow defines the period over which spend counts towards a limit.
// The zero value is a rolling 24-hour window.
type BudgetWindow struct {
	kind     windowKind
	interval time.Duration
	loc      *time.Location
}

// RollingWindow counts spend from the last d (e.g. the last 30 days).
// A non-positive d means DefaultBudgetInterval.
func RollingWindow(d time.Duration) BudgetWindow {
	return BudgetWindow{kind: windowRolling, interval: d}
}

// CalendarDay counts spend since midnight in loc and resets at the next midnight.
// A nil loc means UTC.
func CalendarDay(loc *time.Location) BudgetWindow {
	return BudgetWindow{kind: windowCalendarDay, loc: loc}
}

// CalendarMonth counts spend since the 1st of the month in loc and resets on the 1st of the next month.
// A nil loc means UTC.
func CalendarMonth(loc *time.Location) BudgetWindow {
	return BudgetWindow{kind: windowCalendarMonth, loc: loc}
}

func (w BudgetWindow) rollingInterval() time.Duration {
	if w.interval <= 0 {
		return DefaultBudgetInterval
	}
	return w.interval
}

func (w BudgetWindow) location() *time.Location {
	if w.loc == nil {
		return time.UTC
	}
	return w.loc
}

// Location returns the time zone a calendar window resets in, or nil for a
// rolling window, which doesn't depend on one.
func (w BudgetWindow) Location() *time.Location {
	if w.kind == windowRolling {
		return nil
	}
	return w.location()
}

// String describes the window, e.g. "rolling 24h0m0s" or "calendar month
// in Africa/Lagos". For time.Local, which means a different zone on each
// server, it includes the zone and offset in effect now.
func (w BudgetWindow) String() string {
	switch w.kind {
	case windowCalendarDay:
		return "calendar day in " + locationName(w.location())
	case windowCalendarMonth:
		return "calendar month in " + locationName(w.location())
	default:
		return "rolling " + w.rollingInterval().String()
	}
}

// locationName names loc. time.Local is named by the zone it stands for
// on this machine, since "Local" means something different on each server.
func locationName(loc *time.Location) string {
	if loc != time.Local {
		return loc.String()
	}
	return fmt.Sprintf("Local (%s)", time.Now().In(loc).Format("MST -07:00"))
}

// Start returns the earliest time whose spend still counts at now.
func (w BudgetWindow) Start(now time.Time) time.Time {
	switch w.kind {
	case windowCalendarDay:
		t := now.In(w.location())
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, w.location())
	case windowCalendarMonth:
		t := now.In(w.location())
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, w.location())
	default:
		return now.Add(-w.rollingInterval())
	}
}

// ResetAt returns when spend recorded at oldest stops counting. Calendar
// windows ignore oldest and return the start of the next period after now,
// in the window's time zone. Rolling windows return a time in UTC, so the
// result doesn't depend on the server's time zone.
func (w BudgetWindow) ResetAt(now, oldest time.Time) time.Time {
	switch w.kind {
	case windowCalendarDay:
		return w.Start(now).AddDate(0, 0, 1)
	case windowCalendarMonth:
		return w.Start(now).AddDate(0, 1, 0)
	default:
		if oldest.IsZero() {
			return now.Add(w.rollingInterval()).UTC()
		}
		return oldest.Add(w.rollingInterval()).UTC()
	}
}

// MaxAge is the longest a spend event can keep counting. Stores use it to
// expire data that can no longer affect any window.
func (w BudgetWindow) MaxAge() time.Duration {
	switch w.kind {
	case windowCalendarDay:
		return 25 * time.Hour // a DST day can be 25 hours long
	case windowCalendarMonth:
		return 31*24*time.Hour + time.Hour
	default:
		return w.rollingInterval()
	}
}

// canMerge reports whether a spend event at t can join an in-memory bucket
// that started at first. Merging keeps memory bounded for long windows.
// Rolling windows merge events within 1/1000 of the window; a merged bucket
// expires with its latest event, so it errs towards blocking rather than
// overspending. Calendar windows merge everything in the same period.
func (w BudgetWindow) canMerge(first, t time.Time) bool {
	if w.kind == windowRolling {
		return t.Sub(first) < w.rollingInterval()/1000
	}
	return w.Start(first).Equal(w.Start(t))
}
