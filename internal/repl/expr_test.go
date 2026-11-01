package repl

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"logmetrics/internal/query"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"plain words", "op=upload AND size>50", []string{"op=upload", "AND", "size>50"}},
		{"spaced parens", "( op=upload )", []string{"(", "op=upload", ")"}},
		{"attached parens", "(user=a OR user=b)AND op=upload", []string{"(", "user=a", "OR", "user=b", ")", "AND", "op=upload"}},
		{"nested attached", "NOT((size>50))", []string{"NOT", "(", "(", "size>50", ")", ")"}},
		{"paren inside word", "user=a)b", []string{"user=a", ")", "b"}},
		{"empty parens", "()", []string{"(", ")"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokenize(strings.Fields(tt.in)); !slices.Equal(got, tt.want) {
				t.Errorf("tokenize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMergeComparisons(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"joined terms unchanged", "op=upload AND size>50", []string{"op=upload", "AND", "size>50"}},
		{"spaced chain", "45 <= size < 100", []string{"45<=size<100"}},
		{"joined chain", "45<=size<100", []string{"45<=size<100"}},
		{"partly spaced chain", "45<= size <100", []string{"45<=size<100"}},
		{"chain in parens", "(45 <= size < 100)", []string{"(", "45<=size<100", ")"}},
		{"spaced term", "user = jeff22", []string{"user=jeff22"}},
		{"keywords not merged", "NOT 45 < size AND size <= 9 OR op=upload", []string{"NOT", "45<size", "AND", "size<=9", "OR", "op=upload"}},
		{"keywords any case", "size > and < size", []string{"size>", "and", "<size"}},
		{"parens not merged", "size < ( 5", []string{"size<", "(", "5"}},
		{"operands without operator", "op=upload size>50", []string{"op=upload", "size>50"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeComparisons(tokenize(strings.Fields(tt.in))); !slices.Equal(got, tt.want) {
				t.Errorf("mergeComparisons(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitComparison(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"jeff22", []string{"jeff22"}},
		{"size>50", []string{"size", ">", "50"}},
		{"45<=size<100", []string{"45", "<=", "size", "<", "100"}},
		{"size>", []string{"size", ">", ""}},
		{"<size", []string{"", "<", "size"}},
		{"2020-04-13<=date", []string{"2020-04-13", "<=", "date"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := splitComparison(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("splitComparison(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseFiltersErrorKinds(t *testing.T) {
	const (
		ok = iota
		syntax
		date
		size
		plain
	)
	tests := []struct {
		expr string
		want int
	}{
		{"", ok},
		{"op=upload AND size>50", ok},
		{"date<2020-04-10 OR date>2020-04-20", ok},
		{"date>=2020-04-13 AND date<=2020-04-15", ok},
		{"NOT date>2020-04-15 AND date<2020-04-10", ok},
		{"op=upload size>50", syntax},
		{"(op=upload", syntax},
		{"op=upload AND", syntax},
		{"date>2020-04-15 AND date<2020-04-10", date},
		{"(date>2020-04-15 AND date<2020-04-10) OR op=upload", date},
		{"date=2020-02-30", date},
		{"op=bogus", plain},
		{"op=upload AND size>x", size},
		{"45 <= size < 100", ok},
		{"size>50 AND size<=50", size},
		{"100 < size < 45", size},
		{"2020-04-15 < date < 2020-04-10", date},
		{"45 < size > 10", syntax},
		{"a < user < b", syntax},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			f, err := parseFilters(strings.Fields(tt.expr))
			var se *syntaxError
			var de *dateError
			var ze *sizeError
			got := ok
			switch {
			case errors.As(err, &se):
				got = syntax
			case errors.As(err, &de):
				got = date
			case errors.As(err, &ze):
				got = size
			case err != nil:
				got = plain
			}
			if got != tt.want {
				t.Fatalf("parseFilters(%q) error = %v (kind %d), want kind %d", tt.expr, err, got, tt.want)
			}
			if err == nil && f == nil {
				t.Errorf("parseFilters(%q) returned a nil filter", tt.expr)
			}
		})
	}
}

func TestParseFiltersEmptyMatchesAll(t *testing.T) {
	f, err := parseFilters(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := query.Count(testEvents(), f); got != len(testEvents()) {
		t.Errorf("Count = %d, want %d", got, len(testEvents()))
	}
}
