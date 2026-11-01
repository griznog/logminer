package query

import (
	"os"
	"slices"
	"testing"
	"time"

	"logmetrics/internal/logparse"
)

func ts(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, time.UTC)
}

// sample is a small hand-built event set used by the unit tests.
var sample = []logparse.Event{
	{Time: ts(2020, time.April, 15, 1), User: "jeff22", Op: logparse.Upload, SizeKB: 45},
	{Time: ts(2020, time.April, 15, 23), User: "jeff22", Op: logparse.Upload, SizeKB: 75},
	{Time: ts(2020, time.April, 15, 12), User: "jeff22", Op: logparse.Download, SizeKB: 60},
	{Time: ts(2020, time.April, 16, 0), User: "jeff22", Op: logparse.Upload, SizeKB: 51},
	{Time: ts(2020, time.April, 15, 9), User: "sarah94", Op: logparse.Upload, SizeKB: 50},
	{Time: ts(2020, time.April, 14, 9), User: "Maia86", Op: logparse.Download, SizeKB: 99},
}

func TestCount(t *testing.T) {
	tests := []struct {
		name    string
		events  []logparse.Event
		filters []Filter
		want    int
	}{
		{"no filters", sample, nil, 6},
		{"empty slice", nil, nil, 0},
		{"empty slice with filter", nil, []Filter{ByUser("jeff22")}, 0},
		{"by user", sample, []Filter{ByUser("jeff22")}, 4},
		{"by user case-sensitive", sample, []Filter{ByUser("JEFF22")}, 0},
		{"by user unknown", sample, []Filter{ByUser("nobody")}, 0},
		{"by op upload", sample, []Filter{ByOp(logparse.Upload)}, 4},
		{"by op download", sample, []Filter{ByOp(logparse.Download)}, 2},
		{"size over is strict", sample, []Filter{SizeOver(50)}, 4},
		{"size over large", sample, []Filter{SizeOver(99)}, 0},
		{"on date", sample, []Filter{OnDate(2020, time.April, 15)}, 4},
		{"on date other day", sample, []Filter{OnDate(2020, time.April, 14)}, 1},
		{"on date wrong year", sample, []Filter{OnDate(2021, time.April, 15)}, 0},
		{"upload over 50", sample, []Filter{ByOp(logparse.Upload), SizeOver(50)}, 2},
		{
			"user upload on date",
			sample,
			[]Filter{ByUser("jeff22"), ByOp(logparse.Upload), OnDate(2020, time.April, 15)},
			2,
		},
		{
			"all four filters",
			sample,
			[]Filter{ByUser("jeff22"), ByOp(logparse.Upload), OnDate(2020, time.April, 15), SizeOver(50)},
			1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Count(tc.events, tc.filters...); got != tc.want {
				t.Errorf("Count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCombinators(t *testing.T) {
	jeff, upload, big := ByUser("jeff22"), ByOp(logparse.Upload), SizeOver(50)
	tests := []struct {
		name   string
		filter Filter
		want   int
	}{
		{"and of none matches all", And(), 6},
		{"or of none matches nothing", Or(), 0},
		{"and of one", And(jeff), 4},
		{"or of one", Or(jeff), 4},
		{"and", And(jeff, upload), 3},
		{"and three", And(jeff, upload, big), 2},
		{"or", Or(ByUser("sarah94"), ByUser("Maia86")), 2},
		{"or overlapping counted once", Or(jeff, upload), 5},
		{"not", Not(jeff), 2},
		{"not not", Not(Not(jeff)), 4},
		{"not of and of none", Not(And()), 0},
		{"not of or of none", Not(Or()), 6},
		// NOT op=upload OR (size>50 AND user=jeff22): downloads plus jeff22's big uploads.
		{"nested", Or(Not(upload), And(big, jeff)), 4},
		{"de morgan", Not(Or(jeff, upload)), Count(sample, Not(jeff), Not(upload))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Count(sample, tc.filter); got != tc.want {
				t.Errorf("Count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAndCopiesArgs(t *testing.T) {
	fs := []Filter{ByUser("jeff22")}
	f := And(fs...)
	fs[0] = ByUser("nobody")
	if got := Count(sample, f); got != 4 {
		t.Errorf("Count = %d after mutating the argument slice, want 4", got)
	}
}

func TestOnDateUsesEventZone(t *testing.T) {
	// 23:30 on Apr 15 in UTC-5 is Apr 16 in UTC; the event's own zone wins.
	zone := time.FixedZone("UTC-5", -5*60*60)
	ev := logparse.Event{Time: time.Date(2020, time.April, 15, 23, 30, 0, 0, zone)}
	if !OnDate(2020, time.April, 15)(ev) {
		t.Error("OnDate(2020-04-15) rejected event on that date in its own zone")
	}
	if OnDate(2020, time.April, 16)(ev) {
		t.Error("OnDate(2020-04-16) accepted event that is Apr 15 in its own zone")
	}
}

func TestDate(t *testing.T) {
	// sample covers Apr 14 (1 event), Apr 15 (4 events) and Apr 16 (1 event).
	tests := []struct {
		name    string
		filters []Filter
		want    int
	}{
		{"eq", []Filter{Date(Eq, 2020, time.April, 15)}, 4},
		{"lt excludes the day", []Filter{Date(Lt, 2020, time.April, 15)}, 1},
		{"le includes the whole day", []Filter{Date(Le, 2020, time.April, 15)}, 5},
		{"gt excludes the whole day", []Filter{Date(Gt, 2020, time.April, 15)}, 1},
		{"ge includes the day", []Filter{Date(Ge, 2020, time.April, 15)}, 5},
		{"lt first day", []Filter{Date(Lt, 2020, time.April, 14)}, 0},
		{"gt last day", []Filter{Date(Gt, 2020, time.April, 16)}, 0},
		{"ge first day", []Filter{Date(Ge, 2020, time.April, 14)}, 6},
		{"le last day", []Filter{Date(Le, 2020, time.April, 16)}, 6},
		{"across years", []Filter{Date(Lt, 2021, time.January, 1)}, 6},
		{"inclusive range", []Filter{Date(Ge, 2020, time.April, 15), Date(Le, 2020, time.April, 16)}, 5},
		{"exclusive range", []Filter{Date(Gt, 2020, time.April, 14), Date(Lt, 2020, time.April, 16)}, 4},
		{"empty range", []Filter{Date(Gt, 2020, time.April, 15), Date(Lt, 2020, time.April, 15)}, 0},
		{"unknown cmp", []Filter{Date(Cmp(99), 2020, time.April, 15)}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Count(sample, tc.filters...); got != tc.want {
				t.Errorf("Count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSize(t *testing.T) {
	// sample sizes: 45, 75, 60, 51, 50, 99.
	tests := []struct {
		name    string
		filters []Filter
		want    int
	}{
		{"eq", []Filter{Size(Eq, 50)}, 1},
		{"eq none", []Filter{Size(Eq, 52)}, 0},
		{"lt is strict", []Filter{Size(Lt, 50)}, 1},
		{"le includes the bound", []Filter{Size(Le, 50)}, 2},
		{"gt is strict", []Filter{Size(Gt, 50)}, 4},
		{"ge includes the bound", []Filter{Size(Ge, 50)}, 5},
		{"gt matches size over", []Filter{Size(Gt, 99)}, Count(sample, SizeOver(99))},
		{"half-open range", []Filter{Size(Ge, 45), Size(Lt, 60)}, 3},
		{"empty range", []Filter{Size(Gt, 50), Size(Lt, 51)}, 0},
		{"unknown cmp", []Filter{Size(Cmp(99), 50)}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Count(sample, tc.filters...); got != tc.want {
				t.Errorf("Count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDateUsesEventZone(t *testing.T) {
	// 23:30 on Apr 15 in UTC-5 is Apr 16 in UTC; the event's own zone wins.
	zone := time.FixedZone("UTC-5", -5*60*60)
	ev := logparse.Event{Time: time.Date(2020, time.April, 15, 23, 30, 0, 0, zone)}
	if !Date(Le, 2020, time.April, 15)(ev) {
		t.Error("Date(<=2020-04-15) rejected event on that date in its own zone")
	}
	if Date(Ge, 2020, time.April, 16)(ev) {
		t.Error("Date(>=2020-04-16) accepted event that is Apr 15 in its own zone")
	}
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name    string
		events  []logparse.Event
		filters []Filter
		want    []int // indexes into events, in expected order
	}{
		{"no filters keeps order", sample, nil, []int{0, 1, 2, 3, 4, 5}},
		{"empty slice", nil, nil, nil},
		{"by user", sample, []Filter{ByUser("jeff22")}, []int{0, 1, 2, 3}},
		{"combined", sample, []Filter{ByOp(logparse.Upload), SizeOver(50)}, []int{1, 3}},
		{"date range", sample, []Filter{Date(Ge, 2020, time.April, 14), Date(Lt, 2020, time.April, 15)}, []int{5}},
		{"no match", sample, []Filter{ByUser("nobody")}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var want []logparse.Event
			for _, i := range tc.want {
				want = append(want, tc.events[i])
			}
			got := Select(tc.events, tc.filters...)
			if !slices.Equal(got, want) {
				t.Errorf("Select = %v, want %v", got, want)
			}
		})
	}
}

func TestDistinctUsers(t *testing.T) {
	tests := []struct {
		name    string
		events  []logparse.Event
		filters []Filter
		want    int
	}{
		{"no filters", sample, nil, 3},
		{"empty slice", nil, nil, 0},
		{"by op upload", sample, []Filter{ByOp(logparse.Upload)}, 2},
		{"by op download", sample, []Filter{ByOp(logparse.Download)}, 2},
		{"by user", sample, []Filter{ByUser("sarah94")}, 1},
		{"size over", sample, []Filter{SizeOver(60)}, 2},
		{"on date", sample, []Filter{OnDate(2020, time.April, 15)}, 2},
		{"no match", sample, []Filter{ByUser("nobody")}, 0},
		{"combined", sample, []Filter{ByOp(logparse.Download), SizeOver(60)}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DistinctUsers(tc.events, tc.filters...); got != tc.want {
				t.Errorf("DistinctUsers = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestUniqueUsers(t *testing.T) {
	tests := []struct {
		name    string
		events  []logparse.Event
		filters []Filter
		want    []string
	}{
		// Byte-wise ascending: upper case sorts before lower case.
		{"no filters sorted", sample, nil, []string{"Maia86", "jeff22", "sarah94"}},
		{"empty slice", nil, nil, nil},
		{"by op upload", sample, []Filter{ByOp(logparse.Upload)}, []string{"jeff22", "sarah94"}},
		{"by op download", sample, []Filter{ByOp(logparse.Download)}, []string{"Maia86", "jeff22"}},
		{"size over", sample, []Filter{SizeOver(60)}, []string{"Maia86", "jeff22"}},
		{"on date", sample, []Filter{OnDate(2020, time.April, 15)}, []string{"jeff22", "sarah94"}},
		{"no match", sample, []Filter{ByUser("nobody")}, nil},
		{"combined", sample, []Filter{ByOp(logparse.Download), SizeOver(60)}, []string{"Maia86"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UniqueUsers(tc.events, tc.filters...)
			if len(got) != len(tc.want) || (len(got) > 0 && !slices.Equal(got, tc.want)) {
				t.Errorf("UniqueUsers = %q, want %q", got, tc.want)
			}
			if n := DistinctUsers(tc.events, tc.filters...); n != len(got) {
				t.Errorf("DistinctUsers = %d, want len(UniqueUsers) = %d", n, len(got))
			}
		})
	}
}

func TestUniqueOps(t *testing.T) {
	both := []logparse.Operation{logparse.Download, logparse.Upload}
	tests := []struct {
		name    string
		events  []logparse.Event
		filters []Filter
		want    []logparse.Operation
	}{
		{"no filters sorted", sample, nil, both},
		{"empty slice", nil, nil, nil},
		{"by user", sample, []Filter{ByUser("jeff22")}, both},
		{"by user upload only", sample, []Filter{ByUser("sarah94")}, []logparse.Operation{logparse.Upload}},
		{"size over", sample, []Filter{SizeOver(75)}, []logparse.Operation{logparse.Download}},
		{"on date", sample, []Filter{OnDate(2020, time.April, 16)}, []logparse.Operation{logparse.Upload}},
		{"no match", sample, []Filter{SizeOver(1000)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UniqueOps(tc.events, tc.filters...)
			if len(got) != len(tc.want) || (len(got) > 0 && !slices.Equal(got, tc.want)) {
				t.Errorf("UniqueOps = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestChallengeAnswers checks the three challenge questions against the real
// log. Expected values were computed independently with shell tools:
//
//	tail -n +2 server_log.csv | cut -d, -f2 | sort -u | wc -l          # 6
//	awk -F, '$3=="upload" && $4>50' server_log.csv | wc -l             # 140
//	grep 'Apr 15' server_log.csv | grep -c 'jeff22,upload'             # 3
func TestChallengeAnswers(t *testing.T) {
	f, err := os.Open("../../server_log.csv")
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	events, err := logparse.Parse(f)
	if err != nil {
		t.Fatalf("parse log: %v", err)
	}

	if got, want := DistinctUsers(events), 6; got != want {
		t.Errorf("distinct users = %d, want %d", got, want)
	}
	if got, want := Count(events, ByOp(logparse.Upload), SizeOver(50)), 140; got != want {
		t.Errorf("uploads over 50kB = %d, want %d", got, want)
	}
	if got, want := Count(events, And(ByOp(logparse.Upload), SizeOver(50))), 140; got != want {
		t.Errorf("And(upload, size>50) = %d, want %d", got, want)
	}
	got := Count(events, ByUser("jeff22"), ByOp(logparse.Upload), OnDate(2020, time.April, 15))
	if want := 3; got != want {
		t.Errorf("jeff22 uploads on 2020-04-15 = %d, want %d", got, want)
	}
}
