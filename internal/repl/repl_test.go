package repl

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"logmetrics/internal/logparse"
)

func testEvents() []logparse.Event {
	day := func(d, h int) time.Time { return time.Date(2020, time.April, d, h, 0, 0, 0, time.UTC) }
	return []logparse.Event{
		{Time: day(15, 1), User: "jeff22", Op: logparse.Upload, SizeKB: 60},
		{Time: day(15, 2), User: "jeff22", Op: logparse.Upload, SizeKB: 10},
		{Time: day(15, 3), User: "jeff22", Op: logparse.Download, SizeKB: 70},
		{Time: day(16, 1), User: "jeff22", Op: logparse.Upload, SizeKB: 80},
		{Time: day(15, 4), User: "sarah94", Op: logparse.Upload, SizeKB: 50},
		{Time: day(14, 5), User: "Maia86", Op: logparse.Download, SizeKB: 51},
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // expected output after stripping prompts
	}{
		{"users all", "users\nquit\n", "3\n"},
		{"count all", "count\nquit\n", "6\n"},
		{"users filtered", "users op=upload\n", "2\n\n"},
		{"count by user", "count user=jeff22\n", "4\n\n"},
		{"count by op", "count op=download\n", "2\n\n"},
		{"size strictly greater", "count size>50\n", "4\n\n"},
		{"count by date", "count date=2020-04-15\n", "4\n\n"},
		{"combined Q2", "count op=upload AND size>50\n", "2\n\n"},
		{"combined Q3", "count user=jeff22 AND op=upload AND date=2020-04-15\n", "2\n\n"},
		{"no match", "count user=nobody\n", "0\n\n"},
		{"blank lines ignored", "\n   \ncount\nexit\n", "6\n"},
		{"unknown command", "frob\ncount\n", "error: unknown command \"frob\" (type \"help\" for usage)\n6\n\n"},
		{"bad op", "count op=delete\n", "error: invalid op \"delete\": want upload or download\n\n"},
		{"bad size", "count size>abc\n", "error: invalid size \"abc\": want an integer number of kB\n" + sizeHelpText + "\n"},
		{"bad date", "count date=04/15/2020\n", "error: invalid date \"04/15/2020\": want YYYY-MM-DD\n" + dateHelpText + "\n"},
		{"unknown filter key", "users color=red\n", "error: unknown filter \"color\" (type \"help\" for usage)\n\n"},
		{"malformed term", "users jeff22\n", "error: invalid filter \"jeff22\" (type \"help\" for usage)\n\n"},
		{"empty user", "count user=\n", "error: empty user name in \"user=\"\n\n"},
		// Lists are sorted byte-wise; an empty list prints "(none)".
		{"users list", "users list\n", "Maia86\njeff22\nsarah94\n\n"},
		{"ops list", "ops list\n", "download\nupload\n\n"},
		{"ops count", "ops\n", "2\n\n"},
		{"ops count filtered", "ops user=sarah94\n", "1\n\n"},
		{"users list filtered", "users list op=download\n", "Maia86\njeff22\n\n"},
		{"users list combined", "users list op=upload AND size>50\n", "jeff22\n\n"},
		{"users list empty", "users list date=2021-01-01\n", "(none)\n\n"},
		{"ops list empty", "ops list user=nobody\n", "(none)\n\n"},
		{"users list bad filter", "users list bogus=1\n", "error: unknown filter \"bogus\" (type \"help\" for usage)\n\n"},
		{"list not first", "users op=upload AND list\n", "error: invalid filter \"list\" (type \"help\" for usage)\n\n"},
		{"count has no list", "count list\n", "error: invalid filter \"list\" (type \"help\" for usage)\n\n"},
		// Date operators compare whole days: Apr 14 has 1 event, Apr 15 has 4,
		// Apr 16 has 1.
		{"date lt", "count date<2020-04-15\n", "1\n\n"},
		{"date le", "count date<=2020-04-15\n", "5\n\n"},
		{"date gt", "count date>2020-04-15\n", "1\n\n"},
		{"date ge", "count date>=2020-04-15\n", "5\n\n"},
		{"date range", "count date>2020-04-14 AND date<=2020-04-16\n", "5\n\n"},
		{"date single-day range", "count date>=2020-04-15 AND date<=2020-04-15\n", "4\n\n"},
		{"date range users", "users list date>=2020-04-15 AND date<2020-04-16\n", "jeff22\nsarah94\n\n"},
		// select prints matching records in file order as CSV.
		{
			"select",
			"select user=jeff22 AND op=upload AND date=2020-04-15\n",
			recordHeader + "\n" +
				"Wed Apr 15 01:00:00 UTC 2020,jeff22,upload,60\n" +
				"Wed Apr 15 02:00:00 UTC 2020,jeff22,upload,10\n" +
				"2 records\n\n",
		},
		{
			"select one",
			"select date<2020-04-15\n",
			recordHeader + "\nTue Apr 14 05:00:00 UTC 2020,Maia86,download,51\n1 record\n\n",
		},
		{"select none", "select user=nobody\n", "0 records\n\n"},
		{"select bad filter", "select op=bogus\n", "error: invalid op \"bogus\": want upload or download\n\n"},
		{"select bad date", "select date>=abc\n", "error: invalid date \"abc\": want YYYY-MM-DD\n" + dateHelpText + "\n"},
		{"help", "help\nquit\n", helpText},
		{"quit stops reading", "quit\ncount\n", ""},
		{"EOF without newline", "count", "6\n\n"},
		{"empty input", "", "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(strings.NewReader(tt.input), &out, testEvents()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if got := stripPrompts(out.String()); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

// stripPrompts removes prompts, which appear at the start of each output line.
func stripPrompts(s string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		for strings.HasPrefix(l, prompt) {
			l = l[len(prompt):]
		}
		lines[i] = l
	}
	return strings.Join(lines, "")
}

func TestRunDateErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		err   string // expected error line, without "error: " or newline
	}{
		{"bad month and day", "count date>=2020-13-45", `invalid date "2020-13-45": want YYYY-MM-DD`},
		{"not a date", "count date>=abc", `invalid date "abc": want YYYY-MM-DD`},
		{"empty value", "count date<", `invalid date "": want YYYY-MM-DD`},
		{"unknown operator", "count date!=2020-04-15", `unknown date operator in "date!=2020-04-15": want =, <, <=, > or >=`},
		{"reversed operator", "count date=>2020-04-15", `invalid date ">2020-04-15": want YYYY-MM-DD`},
		{
			"disjoint range",
			"count date>2020-04-15 AND date<2020-04-10",
			"empty date range: no day satisfies date>2020-04-15 AND date<2020-04-10",
		},
		{
			"touching exclusive bound",
			"users date>2020-04-15 AND date<=2020-04-15",
			"empty date range: no day satisfies date>2020-04-15 AND date<=2020-04-15",
		},
		{
			"two different days",
			"select date=2020-04-10 AND date=2020-04-11",
			"empty date range: no day satisfies date=2020-04-10 AND date=2020-04-11",
		},
		{
			"adjacent exclusive bounds",
			"count date>2020-04-14 AND date<2020-04-15 AND user=jeff22",
			"empty date range: no day satisfies date>2020-04-14 AND date<2020-04-15",
		},
		// Each AND chain is checked on its own, at any depth.
		{
			"chain in parentheses under OR",
			"count (date>2020-04-15 AND date<2020-04-10) OR op=upload",
			"empty date range: no day satisfies date>2020-04-15 AND date<2020-04-10",
		},
		{
			"chain in OR branch without parentheses",
			"count op=upload OR date=2020-04-10 AND user=jeff22 AND date=2020-04-11",
			"empty date range: no day satisfies date=2020-04-10 AND date=2020-04-11",
		},
		{
			"chain under NOT",
			"count NOT (date>2020-04-15 AND date<2020-04-10)",
			"empty date range: no day satisfies date>2020-04-15 AND date<2020-04-10",
		},
		{
			"nested AND groups are flattened",
			"count date>2020-04-15 AND (op=upload AND date<2020-04-10)",
			"empty date range: no day satisfies date>2020-04-15 AND date<2020-04-10",
		},
		{
			"two AND groups are flattened",
			"count (date>2020-04-15 AND op=upload) AND (user=jeff22 AND date<2020-04-10)",
			"empty date range: no day satisfies date>2020-04-15 AND date<2020-04-10",
		},
		{
			"deeply nested",
			"count user=jeff22 OR (op=upload AND NOT (size>50 OR (date=2020-04-10 AND date>2020-04-10)))",
			"empty date range: no day satisfies date=2020-04-10 AND date>2020-04-10",
		},
		{
			"keywords case-insensitive",
			"count date>2020-04-15 and date<2020-04-10",
			"empty date range: no day satisfies date>2020-04-15 AND date<2020-04-10",
		},
		// Chained date comparisons are two date terms in one AND chain.
		{
			"chain disjoint",
			"count 2020-04-15 < date < 2020-04-10",
			"empty date range: no day satisfies date>2020-04-15 AND date<2020-04-10",
		},
		{
			"chain adjacent exclusive bounds",
			"count 2020-04-14<date<2020-04-15",
			"empty date range: no day satisfies date>2020-04-14 AND date<2020-04-15",
		},
		{
			"chain with explicit AND",
			"count 2020-04-10 <= date <= 2020-04-20 AND date>2020-04-20",
			"empty date range: no day satisfies date>=2020-04-10 AND date<=2020-04-20 AND date>2020-04-20",
		},
		{
			"chain under NOT",
			"count NOT 2020-04-15 < date < 2020-04-10",
			"empty date range: no day satisfies date>2020-04-15 AND date<2020-04-10",
		},
		{"chain bad literal", "count 2020-04-13 <= date < tomorrow", `invalid date "tomorrow": want YYYY-MM-DD`},
		{"literal left bad date", "count abc < date", `invalid date "abc": want YYYY-MM-DD`},
		{"bad date inside expression", "count op=upload AND (date>=abc OR user=x)", `invalid date "abc": want YYYY-MM-DD`},
		{"bad date under NOT", "count NOT date!2020-04-15", `unknown date operator in "date!2020-04-15": want =, <, <=, > or >=`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(strings.NewReader(tt.input+"\n"), &out, testEvents()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			want := "error: " + tt.err + "\n" + dateHelpText + "\n"
			if got := stripPrompts(out.String()); got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRunPlainErrorsOmitHelp(t *testing.T) {
	inputs := []string{
		"count op=bogus", "users color=red", "count user=", "ops list date",
		"count op=upload AND op=bogus", "count (user= OR op=upload)",
		"users op=upload AND list", "count list", "count size", "count 45 < AND op=upload",
		"count jeff22 = user", "count x < op",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(strings.NewReader(input+"\n"), &out, testEvents()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			got := out.String()
			if !strings.Contains(got, "error: ") {
				t.Errorf("output = %q, want an error", got)
			}
			if strings.Contains(got, dateHelpText) {
				t.Errorf("output = %q, should not contain the date help", got)
			}
			if strings.Contains(got, logicHelpText) {
				t.Errorf("output = %q, should not contain the logic help", got)
			}
			if strings.Contains(got, sizeHelpText) {
				t.Errorf("output = %q, should not contain the size help", got)
			}
		})
	}
}

func TestRunExpressions(t *testing.T) {
	// testEvents: 0 jeff22 up 60 Apr15, 1 jeff22 up 10 Apr15, 2 jeff22 down 70
	// Apr15, 3 jeff22 up 80 Apr16, 4 sarah94 up 50 Apr15, 5 Maia86 down 51 Apr14.
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Precedence: NOT binds tighter than AND, which binds tighter than OR.
		// (NOT up) OR (big AND jeff22) = {0,2,3,5}.
		{"not and or precedence", "count NOT op=upload OR size>50 AND user=jeff22", "4"},
		// jeff22 OR (down AND >60) = {0,1,2,3}; (jeff22 OR down) AND >60 would be 2.
		{"and before or", "count user=jeff22 OR op=download AND size>60", "4"},
		{"and before or, parenthesised", "count (user=jeff22 OR op=download) AND size>60", "2"},
		// (NOT jeff22) AND up = {4}; NOT (jeff22 AND up) would be 3.
		{"not before and", "count NOT user=jeff22 AND op=upload", "1"},
		{"not of group", "count NOT (user=jeff22 AND op=upload)", "3"},
		{"double not", "count NOT NOT user=jeff22", "4"},
		{"or chain", "count user=sarah94 OR user=Maia86 OR size>75", "3"},
		{"nested groups", "count ((user=jeff22 AND (op=download OR size>70)) OR (user=Maia86))", "3"},
		{"help example", "count (user=jeff22 OR user=Maia86) AND op=upload AND NOT size>50", "1"},
		{"select help example", "count NOT op=download AND date>=2020-04-16", "1"},
		// Parentheses split from neighbouring text.
		{"no space after close paren", "count (user=jeff22 OR user=Maia86)AND op=upload", "3"},
		{"no space anywhere", "count NOT(op=upload)AND(size>50)", "2"},
		{"redundant parens", "count ((user=sarah94))", "1"},
		{"keywords lower case", "count user=jeff22 and op=upload or user=Maia86", "4"},
		{"keywords mixed case", "count nOt op=upload AnD size>60", "1"},
		// Date terms are combined only within one AND chain.
		{"date or is not a range", "count date<2020-04-15 OR date>2020-04-15", "2"},
		{"date inclusive range", "count date>=2020-04-13 AND date<=2020-04-15", "5"},
		{"date under not is not a bound", "count NOT date>2020-04-15 AND date>2020-04-15", "0"},
		{"date across or in and", "count (date>2020-04-15 OR op=upload) AND date<2020-04-10", "0"},
		{"empty expression after list", "users list", "Maia86\njeff22\nsarah94"},
		{"users list expression", "users list op=download OR user=sarah94", "Maia86\njeff22\nsarah94"},
		{"ops list expression", "ops list NOT op=upload", "download"},
		{"users count expression", "users NOT user=jeff22", "2"},
		{
			"select expression",
			"select user=Maia86 OR (user=jeff22 AND date>2020-04-15)",
			recordHeader + "\n" +
				"Thu Apr 16 01:00:00 UTC 2020,jeff22,upload,80\n" +
				"Tue Apr 14 05:00:00 UTC 2020,Maia86,download,51\n" +
				"2 records",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(strings.NewReader(tt.input+"\n"), &out, testEvents()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if got, want := stripPrompts(out.String()), tt.want+"\n\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRunSyntaxErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		err   string // expected error line, without "error: " or newline
	}{
		{"missing AND", "count op=upload size>50", `missing AND/OR between "op=upload" and "size>50": an operator is required between filters`},
		{"missing AND before group", "count user=a (op=upload)", `missing AND/OR between "user=a" and "(": an operator is required between filters`},
		{"missing AND after group", "count (op=upload) NOT size>50", `missing AND/OR between ")" and "NOT": an operator is required between filters`},
		{"missing AND with list", "users list op=upload list", `missing AND/OR between "op=upload" and "list": an operator is required between filters`},
		{"missing close paren", "count (op=upload", `unbalanced parentheses: missing ")"`},
		{"missing nested close paren", "count ((op=upload) OR size>50", `unbalanced parentheses: missing ")"`},
		{"extra close paren", "count op=upload)", `unbalanced parentheses: unexpected ")"`},
		{"lone close paren", "count )", `unbalanced parentheses: unexpected ")"`},
		{"empty parens", "count ()", `empty parentheses "()"`},
		{"empty parens after AND", "count op=upload AND ( )", `empty parentheses "()"`},
		{"lone open paren", "count (", `expected a filter after "("`},
		{"trailing AND", "count op=upload AND", `expected a filter after "AND"`},
		{"trailing OR", "count op=upload or", `expected a filter after "or"`},
		{"leading OR", "count OR op=upload", `expected a filter before "OR"`},
		{"leading AND", "select AND op=upload", `expected a filter before "AND"`},
		{"lone NOT", "count NOT", `expected a filter after "NOT"`},
		{"NOT before close", "count (NOT) AND op=upload", `expected a filter after "NOT", found ")"`},
		{"double operator", "count op=upload AND OR size>50", `expected a filter after "AND", found "OR"`},
		{"operator after open", "count (AND op=upload)", `expected a filter after "(", found "AND"`},
		{"chain opposite directions", "count 45 < size > 10", `invalid chained comparison "45<size>10": both operators must be <, <= or both >, >=, as in 45 <= size < 100`},
		{"chain opposite directions joined", "count 2020-04-10<=date>=2020-04-01", `invalid chained comparison "2020-04-10<=date>=2020-04-01": both operators must be <, <= or both >, >=, as in 45 <= size < 100`},
		{"chain with equals", "count 45 = size = 45", `invalid chained comparison "45=size=45": both operators must be <, <= or both >, >=, as in 45 <= size < 100`},
		{"chain with unknown operator", "count 45 != size < 100", `invalid chained comparison "45!=size<100": both operators must be <, <= or both >, >=, as in 45 <= size < 100`},
		{"chain on user", "count a < user < b", `invalid chained comparison "a<user<b": only size and date support chains, as in 45 <= size < 100`},
		{"chain on op", "count (a<=op<=b)", `invalid chained comparison "a<=op<=b": only size and date support chains, as in 45 <= size < 100`},
		{"chain too long", "count 1 < size < 5 < 9", `invalid chained comparison "1<size<5<9": at most two operators, as in 45 <= size < 100`},
		{"chain then missing AND", "count 45 <= size < 100 op=upload", `missing AND/OR between "45<=size<100" and "op=upload": an operator is required between filters`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(strings.NewReader(tt.input+"\n"), &out, testEvents()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			want := "error: " + tt.err + "\n" + logicHelpText + "\n"
			if got := stripPrompts(out.String()); got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRunSizeErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		err   string // expected error line, without "error: " or newline
	}{
		{"not a number", "count size>abc", `invalid size "abc": want an integer number of kB`},
		{"not a number after AND", "select op=upload AND size>x", `invalid size "x": want an integer number of kB`},
		{"not a number under NOT", "count NOT size>x", `invalid size "x": want an integer number of kB`},
		{"literal left not a number", "count abc < size", `invalid size "abc": want an integer number of kB`},
		{"literal left equals not a number", "count x=size", `invalid size "x": want an integer number of kB`},
		{"chain bad upper", "count 45 <= size < lots", `invalid size "lots": want an integer number of kB`},
		{"empty value", "count size<=", `invalid size "": want an integer number of kB`},
		{"unknown operator", "count size!=5", `unknown size operator in "size!=5": want =, <, <=, > or >=`},
		{"reversed operator", "count size=>5", `invalid size ">5": want an integer number of kB`},
		{"chain disjoint", "count 100 < size < 45", "empty size range: no size satisfies size>100 AND size<45"},
		{"chain disjoint descending", "count 45 > size > 100", "empty size range: no size satisfies size<45 AND size>100"},
		{"chain integer gap", "count 50 < size < 51", "empty size range: no size satisfies size>50 AND size<51"},
		{"chain equal exclusive", "count 50<size<=50", "empty size range: no size satisfies size>50 AND size<=50"},
		{"touching exclusive bound", "count size>50 AND size<=50", "empty size range: no size satisfies size>50 AND size<=50"},
		{"ge and lt same", "count size>=10 AND size<10", "empty size range: no size satisfies size>=10 AND size<10"},
		{"integer gap", "count size>50 AND size<51", "empty size range: no size satisfies size>50 AND size<51"},
		{"two different sizes", "count size=1 AND op=upload AND size=2", "empty size range: no size satisfies size=1 AND size=2"},
		{"literal left terms", "count 50 < size AND 51 > size", "empty size range: no size satisfies size>50 AND size<51"},
		{"above max int", "count size>9223372036854775807 AND op=upload", "empty size range: no size satisfies size>9223372036854775807"},
		{
			"chain with explicit AND",
			"count 45 <= size < 100 AND size>=100",
			"empty size range: no size satisfies size>=45 AND size<100 AND size>=100",
		},
		{"chain in parentheses", "count (100 < size < 45)", "empty size range: no size satisfies size>100 AND size<45"},
		{"chain under NOT", "count NOT (100<size<45)", "empty size range: no size satisfies size>100 AND size<45"},
		{"chain under bare NOT", "count NOT 100<size<45", "empty size range: no size satisfies size>100 AND size<45"},
		{"chain in OR branch", "count op=upload OR 100 < size < 45", "empty size range: no size satisfies size>100 AND size<45"},
		{
			"nested AND groups are flattened",
			"count size>50 AND (op=upload AND size<51)",
			"empty size range: no size satisfies size>50 AND size<51",
		},
		{
			"deeply nested",
			"count user=jeff22 OR (op=upload AND NOT (date=2020-04-10 OR (size=10 AND size>10)))",
			"empty size range: no size satisfies size=10 AND size>10",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(strings.NewReader(tt.input+"\n"), &out, testEvents()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			want := "error: " + tt.err + "\n" + sizeHelpText + "\n"
			if got := stripPrompts(out.String()); got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRunComparisons(t *testing.T) {
	// testEvents sizes: 60, 10, 70, 80, 50, 51; days: 15, 15, 15, 16, 15, 14.
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"size eq", "count size=50", "1"},
		{"size lt", "count size<50", "1"},
		{"size le", "count size<=50", "2"},
		{"size gt", "count size>50", "4"},
		{"size ge", "count size>=50", "5"},
		{"size spaced", "count size >= 50", "5"},
		{"size spaced op attached", "count size >=50", "5"},
		{"literal left lt", "count 50 < size", "4"},
		{"literal left le", "count 50<=size", "5"},
		{"literal left gt", "count 50 > size", "1"},
		{"literal left ge", "count 50 >= size", "2"},
		{"literal left eq", "count 50 = size", "1"},
		{"user spaced", "count user = jeff22", "4"},
		// {50, 51, 60} lie in [50, 70).
		{"chain ascending", "count 50 <= size < 70", "3"},
		{"chain joined", "count 50<=size<70", "3"},
		{"chain partly joined", "count 50<= size <70", "3"},
		{"chain descending", "count 70 > size >= 50", "3"},
		{"chain both strict", "count 50 < size < 70", "2"},
		{"chain both inclusive", "count 50 <= size <= 70", "4"},
		{"chain single value", "count 50 <= size <= 50", "1"},
		{"chain in parentheses", "count (50 <= size < 70)", "3"},
		{"chain in spaced parentheses", "count ( 50 <= size < 70 )", "3"},
		{"chain AND", "count 50 <= size < 70 AND op=upload", "2"},
		{"AND chain", "count op=upload AND 50<=size<70", "2"},
		{"chain OR", "count 50 <= size < 70 OR user=jeff22", "6"},
		{"NOT chain", "count NOT 50 <= size < 70", "3"},
		{"NOT parenthesised chain", "count NOT (50 <= size < 70) OR op=download", "4"},
		{"chain lower case keywords", "count 50 <= size < 70 and not op=download", "2"},
		{"two chains", "count 10 <= size <= 60 AND 50 < size < 100", "2"},
		{"date chain", "count 2020-04-14 < date <= 2020-04-16", "5"},
		{"date chain joined", "count 2020-04-15<=date<=2020-04-15", "4"},
		{"date chain descending", "count 2020-04-16 > date >= 2020-04-14", "5"},
		{"date literal left", "count 2020-04-15 < date", "1"},
		{"date spaced", "count date >= 2020-04-15", "5"},
		{"date and size chains", "count 2020-04-15 <= date <= 2020-04-15 AND 50 <= size < 70", "2"},
		// Ranges are only combined within one AND chain.
		{"disjoint chains under OR", "count 100 < size OR size < 45", "1"},
		{"chain under NOT not a bound", "count NOT (50 <= size < 70) AND size=60", "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Run(strings.NewReader(tt.input+"\n"), &out, testEvents()); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if got, want := stripPrompts(out.String()), tt.want+"\n\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

// TestRunChainsMatchExplicitAnd checks that each chained form selects exactly
// the same records as the explicit AND it desugars to.
func TestRunChainsMatchExplicitAnd(t *testing.T) {
	pairs := [][2]string{
		{"45 <= size < 100", "size>=45 AND size<100"},
		{"100 > size >= 45", "size<100 AND size>=45"},
		{"10<size<=60", "size>10 AND size<=60"},
		{"60 >= size > 10", "size<=60 AND size>10"},
		{"50 < size", "size>50"},
		{"2020-04-14 <= date < 2020-04-16", "date>=2020-04-14 AND date<2020-04-16"},
		{"NOT (45 <= size < 100) OR op=download", "NOT (size>=45 AND size<100) OR op=download"},
	}
	run := func(expr string) string {
		var out bytes.Buffer
		if err := Run(strings.NewReader("select "+expr+"\n"), &out, testEvents()); err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		return out.String()
	}
	for _, p := range pairs {
		t.Run(p[0], func(t *testing.T) {
			if got, want := run(p[0]), run(p[1]); got != want {
				t.Errorf("select %s = %q, want %q (from %s)", p[0], got, want, p[1])
			}
		})
	}
}

// TestRunChallengeAnswers asks the three challenge questions through the
// prompt against the real log; see query.TestChallengeAnswers for how the
// expected values were computed.
func TestRunChallengeAnswers(t *testing.T) {
	f, err := os.Open("../../server_log.csv")
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	events, err := logparse.Parse(f)
	if err != nil {
		t.Fatalf("parse log: %v", err)
	}
	input := "users\ncount op=upload AND size>50\ncount user=jeff22 AND op=upload AND date=2020-04-15\n"
	var out bytes.Buffer
	if err := Run(strings.NewReader(input), &out, events); err != nil {
		t.Fatal(err)
	}
	if got, want := stripPrompts(out.String()), "6\n140\n3\n\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestFormatRecord(t *testing.T) {
	ev := logparse.Event{
		Time:   time.Date(2020, time.April, 15, 23, 5, 9, 0, time.UTC),
		User:   "jeff22",
		Op:     logparse.Download,
		SizeKB: 7,
	}
	if got, want := formatRecord(ev), "Wed Apr 15 23:05:09 UTC 2020,jeff22,download,7"; got != want {
		t.Errorf("formatRecord = %q, want %q", got, want)
	}
}

func TestSelectRoundTrip(t *testing.T) {
	// select output, minus the summary line, must parse back to the same events.
	var out bytes.Buffer
	if err := Run(strings.NewReader("select\n"), &out, testEvents()); err != nil {
		t.Fatal(err)
	}
	csv, summary, _ := strings.Cut(stripPrompts(out.String()), "6 records\n")
	if summary != "\n" {
		t.Fatalf("output = %q, want a trailing \"6 records\" line", out.String())
	}
	got, err := logparse.Parse(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parse select output: %v", err)
	}
	want := testEvents()
	if len(got) != len(want) {
		t.Fatalf("parsed %d events, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Time.Equal(want[i].Time) || got[i].User != want[i].User ||
			got[i].Op != want[i].Op || got[i].SizeKB != want[i].SizeKB {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRunPrompts(t *testing.T) {
	var out bytes.Buffer
	if err := Run(strings.NewReader("count\nquit\n"), &out, testEvents()); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "> 6\n> "; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
