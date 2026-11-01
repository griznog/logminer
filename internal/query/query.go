// Package query answers questions about parsed log events using composable
// filters. Each query is a single pass over the events; a query matches an
// event only when every supplied filter accepts it. Filters combine into
// boolean expressions with And, Or and Not.
package query

import (
	"maps"
	"slices"
	"time"

	"logmetrics/internal/logparse"
)

// Filter reports whether an event should be included in a query.
type Filter func(logparse.Event) bool

// ByUser returns a Filter that accepts events performed by the named user.
// The comparison is exact and case-sensitive.
func ByUser(name string) Filter {
	return func(ev logparse.Event) bool { return ev.User == name }
}

// ByOp returns a Filter that accepts events with the given operation.
func ByOp(op logparse.Operation) Filter {
	return func(ev logparse.Event) bool { return ev.Op == op }
}

// SizeOver returns a Filter that accepts events whose size is strictly
// greater than kb kilobytes. It is equivalent to Size(Gt, kb).
func SizeOver(kb int) Filter {
	return Size(Gt, kb)
}

// Cmp is a comparison operator used by Date and Size.
type Cmp int

// Supported comparisons. Each compares an event's value (its calendar date or
// its size) against a target: Lt means the event's value is strictly less
// than (for dates, before) the target, and so on.
const (
	// Eq accepts events equal to the target.
	Eq Cmp = iota
	// Lt accepts events strictly less than (before) the target.
	Lt
	// Le accepts events less than (before) or equal to the target.
	Le
	// Gt accepts events strictly greater than (after) the target.
	Gt
	// Ge accepts events greater than (after) or equal to the target.
	Ge
)

// compare reports whether c, the result of comparing an event's value with
// a target (negative, zero or positive), satisfies cmp. An unknown cmp is
// never satisfied.
func (cmp Cmp) compare(c int) bool {
	switch cmp {
	case Eq:
		return c == 0
	case Lt:
		return c < 0
	case Le:
		return c <= 0
	case Gt:
		return c > 0
	case Ge:
		return c >= 0
	default:
		return false
	}
}

// Size returns a Filter that compares each event's size in kilobytes against
// kb using cmp; for example Size(Le, 100) accepts sizes of at most 100 kB.
// An unknown cmp accepts nothing.
func Size(cmp Cmp, kb int) Filter {
	return func(ev logparse.Event) bool { return cmp.compare(compareInt(ev.SizeKB, kb)) }
}

// compareInt returns -1, 0 or +1 as a is less than, equal to or greater than b.
func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// Date returns a Filter that compares each event's calendar date, as observed
// in the event timestamp's own time zone, against the given date using cmp.
// Comparison is at day granularity, so Le includes the whole target day and
// Gt excludes all of it. Out-of-range values are normalized as by time.Date.
// An unknown cmp accepts nothing.
func Date(cmp Cmp, year int, month time.Month, day int) Filter {
	target := dayOf(year, month, day)
	return func(ev logparse.Event) bool {
		return cmp.compare(dayOf(ev.Time.Date()).Compare(target))
	}
}

// dayOf returns midnight UTC on the given date, a zone-free key for
// comparing calendar days.
func dayOf(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// OnDate returns a Filter that accepts events that occurred on the given
// calendar date, as observed in the event timestamp's own time zone. It is
// equivalent to Date(Eq, year, month, day).
func OnDate(year int, month time.Month, day int) Filter {
	return Date(Eq, year, month, day)
}

// And returns a Filter that accepts events accepted by every one of filters.
// With no filters it accepts every event.
func And(filters ...Filter) Filter {
	filters = slices.Clone(filters)
	return func(ev logparse.Event) bool { return matches(ev, filters) }
}

// Or returns a Filter that accepts events accepted by at least one of
// filters. With no filters it accepts nothing.
func Or(filters ...Filter) Filter {
	filters = slices.Clone(filters)
	return func(ev logparse.Event) bool {
		for _, f := range filters {
			if f(ev) {
				return true
			}
		}
		return false
	}
}

// Not returns a Filter that accepts exactly the events f rejects.
func Not(f Filter) Filter {
	return func(ev logparse.Event) bool { return !f(ev) }
}

// matches reports whether ev is accepted by every filter.
func matches(ev logparse.Event, filters []Filter) bool {
	for _, f := range filters {
		if !f(ev) {
			return false
		}
	}
	return true
}

// Count returns the number of events accepted by all filters. With no
// filters, every event is counted.
func Count(events []logparse.Event, filters ...Filter) int {
	n := 0
	for _, ev := range events {
		if matches(ev, filters) {
			n++
		}
	}
	return n
}

// Select returns the events accepted by all filters, in their original order.
// With no filters, it returns a copy of all events. It returns nil when
// nothing matches.
func Select(events []logparse.Event, filters ...Filter) []logparse.Event {
	var out []logparse.Event
	for _, ev := range events {
		if matches(ev, filters) {
			out = append(out, ev)
		}
	}
	return out
}

// DistinctUsers returns the number of distinct users among the events
// accepted by all filters. With no filters, it counts all users present.
func DistinctUsers(events []logparse.Event, filters ...Filter) int {
	return len(distinct(events, filters, userOf))
}

// UniqueUsers returns the distinct users among the events accepted by all
// filters, sorted ascending. It returns nil when nothing matches.
func UniqueUsers(events []logparse.Event, filters ...Filter) []string {
	return slices.Sorted(maps.Keys(distinct(events, filters, userOf)))
}

// UniqueOps returns the distinct operations among the events accepted by all
// filters, sorted ascending. It returns nil when nothing matches.
func UniqueOps(events []logparse.Event, filters ...Filter) []logparse.Operation {
	return slices.Sorted(maps.Keys(distinct(events, filters, opOf)))
}

func userOf(ev logparse.Event) string           { return ev.User }
func opOf(ev logparse.Event) logparse.Operation { return ev.Op }

// distinct returns the set of key(ev) values over the events accepted by all
// filters.
func distinct[K comparable](events []logparse.Event, filters []Filter, key func(logparse.Event) K) map[K]struct{} {
	seen := make(map[K]struct{})
	for _, ev := range events {
		if matches(ev, filters) {
			seen[key(ev)] = struct{}{}
		}
	}
	return seen
}
