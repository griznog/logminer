package repl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"logmetrics/internal/logparse"
	"logmetrics/internal/query"
)

// This file parses filter expressions such as
//
//	(user=jeff22 OR user=Maia86) AND op=upload AND NOT size>50
//
// with the grammar, tightest binding first:
//
//	expr := and { OR and }
//	and  := not { AND not }
//	not  := NOT not | '(' expr ')' | term
//	term := user=<name> | op=<operation> | size<op>N | date<op>D
//	      | V <op> size | V <op> size <op> V | V <op> date | V <op> date <op> V
//
// where <op> is =, <, <=, > or >= and D is a date literal, YYYY-MM-DD for a
// whole day or YYYY-MM-DDTHH:MM[:SS] for one second, in UTC. A chained comparison such as
// "45 <= size < 100" means "size>=45 AND size<100"; its operators must both be
// one of <, <=, > and >= and point the same way. Spaces around comparison
// operators are optional. Keywords are case-insensitive. Adjacent operands
// without an operator are an error: AND is never implied.

// syntaxError reports a malformed filter expression. Run prints logicHelpText
// after it.
type syntaxError struct{ msg string }

func (e *syntaxError) Error() string { return e.msg }

// syntaxErrorf returns a *syntaxError with a formatted message.
func syntaxErrorf(format string, args ...any) error {
	return &syntaxError{msg: fmt.Sprintf(format, args...)}
}

// dateError reports an invalid date filter. Run prints dateHelpText after it.
type dateError struct{ msg string }

func (e *dateError) Error() string { return e.msg }

// dateErrorf returns a *dateError with a formatted message.
func dateErrorf(format string, args ...any) error {
	return &dateError{msg: fmt.Sprintf(format, args...)}
}

// sizeError reports an invalid size filter. Run prints sizeHelpText after it.
type sizeError struct{ msg string }

func (e *sizeError) Error() string { return e.msg }

// sizeErrorf returns a *sizeError with a formatted message.
func sizeErrorf(format string, args ...any) error {
	return &sizeError{msg: fmt.Sprintf(format, args...)}
}

// node is a parsed filter expression.
type node interface {
	filter() query.Filter
}

// leaf is a single filter term. date is non-nil for date terms and size for
// size terms, so that the AND chains containing them can be checked for an
// empty range.
type leaf struct {
	f    query.Filter
	date *dateTerm
	size *sizeTerm
}

// notNode negates its operand.
type notNode struct{ x node }

// andNode matches when all of its operands match. Directly nested AND chains
// are flattened into one andNode.
type andNode []node

// orNode matches when any of its operands matches.
type orNode []node

func (n leaf) filter() query.Filter    { return n.f }
func (n notNode) filter() query.Filter { return query.Not(n.x.filter()) }
func (n andNode) filter() query.Filter { return query.And(filters(n)...) }
func (n orNode) filter() query.Filter  { return query.Or(filters(n)...) }

// filters returns the filter of each node.
func filters(nodes []node) []query.Filter {
	fs := make([]query.Filter, len(nodes))
	for i, n := range nodes {
		fs[i] = n.filter()
	}
	return fs
}

// parseFilters parses the words of a filter expression, such as
// ["op=upload", "AND", "size>50"], into a single query filter. An empty
// expression matches every event. Malformed expressions yield a *syntaxError,
// invalid or contradictory date terms a *dateError, invalid or contradictory
// size terms a *sizeError, and other invalid terms a plain error naming the
// term.
func parseFilters(words []string) (query.Filter, error) {
	p := parser{toks: mergeComparisons(tokenize(words))}
	if len(p.toks) == 0 {
		return query.And(), nil
	}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if tok, ok := p.peek(); ok {
		// parseAnd stops only before an operator or ")", and parseOr
		// consumes every OR, so only an unmatched ")" can remain.
		return nil, syntaxErrorf("unbalanced parentheses: unexpected %q", tok)
	}
	return n.filter(), nil
}

// tokenize splits whitespace-separated words into tokens, separating "(" and
// ")" from the text around them, so "(user=a" becomes "(" and "user=a".
func tokenize(words []string) []string {
	var toks []string
	for _, w := range words {
		for w != "" {
			i := strings.IndexAny(w, "()")
			if i < 0 {
				toks = append(toks, w)
				break
			}
			if i > 0 {
				toks = append(toks, w[:i])
			}
			toks = append(toks, w[i:i+1])
			w = w[i+1:]
		}
	}
	return toks
}

// mergeComparisons joins adjacent tokens that belong to one comparison term,
// so that "45", "<=", "size", "<", "100" becomes "45<=size<100" and "size",
// ">", "50" becomes "size>50". A token is joined to the one before it when
// either side of the gap is a comparison operator character. Parentheses and
// the keywords AND, OR and NOT are never joined.
func mergeComparisons(toks []string) []string {
	var out []string
	for _, tok := range toks {
		if n := len(out); n > 0 && !isStructural(tok) && !isStructural(out[n-1]) &&
			(isCmpChar(out[n-1][len(out[n-1])-1]) || isCmpChar(tok[0])) {
			out[n-1] += tok
			continue
		}
		out = append(out, tok)
	}
	return out
}

// isStructural reports whether tok is a parenthesis or an operator keyword.
func isStructural(tok string) bool {
	return tok == "(" || tok == ")" || isOperator(tok) || isKeyword(tok, "NOT")
}

// isCmpChar reports whether c can be part of a comparison operator.
func isCmpChar(c byte) bool { return strings.IndexByte(cmpChars, c) >= 0 }

// cmpChars are the characters comparison operators are made of.
const cmpChars = "=<>!"

// isKeyword reports whether tok is the operator keyword kw, ignoring case.
func isKeyword(tok, kw string) bool { return strings.EqualFold(tok, kw) }

// isOperator reports whether tok is a binary operator keyword.
func isOperator(tok string) bool { return isKeyword(tok, "AND") || isKeyword(tok, "OR") }

// parser is a recursive-descent parser over a token list.
type parser struct {
	toks []string
	pos  int
}

// peek returns the next token without consuming it; ok is false at the end.
func (p *parser) peek() (tok string, ok bool) {
	if p.pos < len(p.toks) {
		return p.toks[p.pos], true
	}
	return "", false
}

// prev returns the most recently consumed token, or "" if there is none.
func (p *parser) prev() string {
	if p.pos == 0 {
		return ""
	}
	return p.toks[p.pos-1]
}

// parseOr parses: and { OR and }.
func (p *parser) parseOr() (node, error) {
	var ops orNode
	for {
		n, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		ops = append(ops, n)
		if tok, ok := p.peek(); !ok || !isKeyword(tok, "OR") {
			break
		}
		p.pos++
	}
	if len(ops) == 1 {
		return ops[0], nil
	}
	return ops, nil
}

// parseAnd parses: not { AND not }. It flattens operands that are themselves
// AND chains (from parentheses and chained comparisons) and rejects the
// chain if its direct date or size terms leave no value that satisfies all of
// them.
func (p *parser) parseAnd() (node, error) {
	var ops andNode
	for {
		n, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		if inner, ok := n.(andNode); ok {
			ops = append(ops, inner...)
		} else {
			ops = append(ops, n)
		}
		tok, ok := p.peek()
		if !ok || tok == ")" || isKeyword(tok, "OR") {
			break
		}
		if !isKeyword(tok, "AND") {
			if d, ok := spacedDateTime(p.prev(), tok); ok {
				return nil, dateErrorf("invalid date %q: join the date and time with T, as in %s", d+" "+tok, d+"T"+tok)
			}
			return nil, syntaxErrorf("missing AND/OR between %q and %q: an operator is required between filters", p.prev(), tok)
		}
		p.pos++
	}
	if len(ops) == 1 {
		return ops[0], nil
	}
	if err := checkRange(ops); err != nil {
		return nil, err
	}
	return ops, nil
}

// spacedDateTime reports whether prev, a term ending in a YYYY-MM-DD date, and
// tok, a time of day, look like a date and time separated by a space, as in
// "date>=2020-04-15 10:00". It returns the date part of prev.
func spacedDateTime(prev, tok string) (string, bool) {
	if len(prev) < len(time.DateOnly) {
		return "", false
	}
	d := prev[len(prev)-len(time.DateOnly):]
	if _, err := time.Parse(time.DateOnly, d); err != nil {
		return "", false
	}
	_, err1 := time.Parse("15:04", tok)
	_, err2 := time.Parse("15:04:05", tok)
	return d, err1 == nil || err2 == nil
}

// parseNot parses: NOT not | '(' expr ')' | term.
func (p *parser) parseNot() (node, error) {
	tok, ok := p.peek()
	switch {
	case !ok:
		return nil, syntaxErrorf("expected a filter after %q", p.prev())
	case tok == ")" && p.prev() == "(":
		return nil, syntaxErrorf("empty parentheses \"()\"")
	case tok == ")" && p.pos == 0:
		return nil, syntaxErrorf("unbalanced parentheses: unexpected %q", tok)
	case isOperator(tok) && p.pos == 0:
		return nil, syntaxErrorf("expected a filter before %q", tok)
	case tok == ")" || isOperator(tok):
		return nil, syntaxErrorf("expected a filter after %q, found %q", p.prev(), tok)
	}
	p.pos++
	switch {
	case isKeyword(tok, "NOT"):
		n, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return notNode{n}, nil
	case tok == "(":
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if next, ok := p.peek(); !ok || next != ")" {
			return nil, syntaxErrorf("unbalanced parentheses: missing \")\"")
		}
		p.pos++
		return n, nil
	default:
		return parseTerm(tok)
	}
}

// parseTerm converts a single filter term such as "user=jeff22",
// "size>50" or "45<=size<100" into a node: a leaf, or for a chained
// comparison an andNode of two leaves. It returns an error naming the term
// if it is invalid.
func parseTerm(term string) (node, error) {
	parts := splitComparison(term)
	if len(parts) == 1 {
		return nil, fmt.Errorf("invalid filter %q (type \"help\" for usage)", term)
	}
	if len(parts) >= 5 && !isField(parts[0]) && isField(parts[2]) && !isRangeField(parts[2]) {
		return nil, syntaxErrorf("invalid chained comparison %q: only size and date support chains, as in 45 <= size < 100", term)
	}
	if isField(parts[0]) || !isRangeField(parts[2]) {
		return parseFieldTerm(term)
	}
	// The literal comes first: rewrite each comparison with the field on
	// the left, e.g. "45<=size" as "size>=45".
	field := parts[2]
	switch len(parts) {
	case 3:
		return parseFieldTerm(field + flipOp(parts[1]) + parts[0])
	case 5:
		lo, hi := parts[1], parts[3]
		if !isInequality(lo) || !isInequality(hi) || lo[0] != hi[0] {
			return nil, syntaxErrorf("invalid chained comparison %q: both operators must be <, <= or both >, >=, as in 45 <= size < 100", term)
		}
		a, err := parseFieldTerm(field + flipOp(lo) + parts[0])
		if err != nil {
			return nil, err
		}
		b, err := parseFieldTerm(field + hi + parts[4])
		if err != nil {
			return nil, err
		}
		chain := andNode{a, b}
		if err := checkRange(chain); err != nil {
			return nil, err
		}
		return chain, nil
	default:
		return nil, syntaxErrorf("invalid chained comparison %q: at most two operators, as in 45 <= size < 100", term)
	}
}

// splitComparison splits term into alternating operands and runs of
// comparison operator characters, always starting and ending with a
// (possibly empty) operand: "45<=size<100" gives ["45", "<=", "size", "<",
// "100"] and "size>" gives ["size", ">", ""].
func splitComparison(term string) []string {
	parts := []string{""}
	for i := 0; i < len(term); i++ {
		c := term[i]
		inOp := len(parts)%2 == 0 // parts alternate operand, operator, ...
		switch {
		case isCmpChar(c) && !inOp, !isCmpChar(c) && inOp:
			parts = append(parts, string(c))
		default:
			parts[len(parts)-1] += string(c)
		}
	}
	if len(parts)%2 == 0 {
		parts = append(parts, "")
	}
	return parts
}

// isField reports whether s names a filter field.
func isField(s string) bool { return s == "user" || s == "op" || isRangeField(s) }

// isRangeField reports whether s names a field that supports ordering
// comparisons, and so may appear on the right of a comparison or in a chain.
func isRangeField(s string) bool { return s == "size" || s == "date" }

// isInequality reports whether op is <, <=, > or >=.
func isInequality(op string) bool { return op == "<" || op == "<=" || op == ">" || op == ">=" }

// flipOp returns the operator that means the same with its operands swapped:
// "45<size" is "size>45". Other operators, including unknown ones, are
// returned unchanged.
func flipOp(op string) string {
	if r, ok := map[string]string{"<": ">", "<=": ">=", ">": "<", ">=": "<="}[op]; ok {
		return r
	}
	return op
}

// parseFieldTerm parses a term written with its field first, such as
// "user=jeff22", "size>=45" or "date<2020-04-15", into a leaf.
func parseFieldTerm(term string) (leaf, error) {
	if rest, ok := strings.CutPrefix(term, "date"); ok && rest != "" && isCmpChar(rest[0]) {
		dt, err := parseDate(term, rest)
		if err != nil {
			return leaf{}, err
		}
		if dt.lit.hasTime {
			return leaf{f: query.DateTime(dt.cmp, dt.lit.start), date: &dt}, nil
		}
		y, m, d := dt.lit.start.Date()
		return leaf{f: query.Date(dt.cmp, y, m, d), date: &dt}, nil
	}
	if rest, ok := strings.CutPrefix(term, "size"); ok && rest != "" && isCmpChar(rest[0]) {
		st, err := parseSize(term, rest)
		if err != nil {
			return leaf{}, err
		}
		return leaf{f: query.Size(st.cmp, st.kb), size: &st}, nil
	}
	key, v, ok := strings.Cut(term, "=")
	if !ok {
		return leaf{}, fmt.Errorf("invalid filter %q (type \"help\" for usage)", term)
	}
	switch key {
	case "user":
		if v == "" {
			return leaf{}, fmt.Errorf("empty user name in %q", term)
		}
		return leaf{f: query.ByUser(v)}, nil
	case "op":
		op := logparse.Operation(v)
		if op != logparse.Upload && op != logparse.Download {
			return leaf{}, fmt.Errorf("invalid op %q: want upload or download", v)
		}
		return leaf{f: query.ByOp(op)}, nil
	default:
		return leaf{}, fmt.Errorf("unknown filter %q (type \"help\" for usage)", key)
	}
}

// cmpOps maps date and size filter operators to comparisons. Two-character
// operators come first so that "<=" is not read as "<" followed by "=".
var cmpOps = []struct {
	op  string
	cmp query.Cmp
}{
	{"<=", query.Le},
	{">=", query.Ge},
	{"=", query.Eq},
	{"<", query.Lt},
	{">", query.Gt},
}

// dateTerm is a parsed date filter term.
type dateTerm struct {
	term string      // the term as written, for error messages
	cmp  query.Cmp   // comparison against lit
	lit  dateLiteral // the target day or second
}

// dateLiteral is a parsed date value: a whole UTC day for YYYY-MM-DD, or a
// whole UTC second for YYYY-MM-DDTHH:MM[:SS]. Either way it denotes the
// half-open span [start, end).
type dateLiteral struct {
	start, end time.Time
	hasTime    bool // the literal gave a time of day, so the span is one second
}

// dateLayouts are the accepted date literal forms, all read as UTC. The time
// forms need the "T" so that the literal stays a single word.
var dateLayouts = []struct {
	layout  string
	hasTime bool
}{
	{time.DateOnly, false},
	{"2006-01-02T15:04", true},
	{"2006-01-02T15:04:05", true},
}

// parseDateLiteral parses v as YYYY-MM-DD, YYYY-MM-DDTHH:MM or
// YYYY-MM-DDTHH:MM:SS. Errors are *dateError values.
func parseDateLiteral(v string) (dateLiteral, error) {
	for _, l := range dateLayouts {
		// time.Parse without a zone yields UTC, the zone of the log. The
		// length check rejects the fractional seconds time.Parse would
		// otherwise accept after the seconds field.
		t, err := time.Parse(l.layout, v)
		if err != nil || len(v) != len(l.layout) {
			continue
		}
		if l.hasTime {
			return dateLiteral{start: t, end: t.Add(time.Second), hasTime: true}, nil
		}
		return dateLiteral{start: t, end: t.AddDate(0, 0, 1)}, nil
	}
	return dateLiteral{}, dateErrorf("invalid date %q: want YYYY-MM-DD or YYYY-MM-DDTHH:MM[:SS]", v)
}

// parseDate parses one date term, given in full as term and with the leading
// "date" removed as rest. Errors are *dateError values.
func parseDate(term, rest string) (dateTerm, error) {
	for _, o := range cmpOps {
		v, ok := strings.CutPrefix(rest, o.op)
		if !ok {
			continue
		}
		lit, err := parseDateLiteral(v)
		if err != nil {
			return dateTerm{}, err
		}
		return dateTerm{term: term, cmp: o.cmp, lit: lit}, nil
	}
	return dateTerm{}, dateErrorf("unknown date operator in %q: want =, <, <=, > or >=", term)
}

// sizeTerm is a parsed size filter term.
type sizeTerm struct {
	term string    // the term as written with size first, for error messages
	cmp  query.Cmp // comparison against kb
	kb   int
}

// parseSize parses one size term, given in full as term and with the leading
// "size" removed as rest. Errors are *sizeError values.
func parseSize(term, rest string) (sizeTerm, error) {
	for _, o := range cmpOps {
		v, ok := strings.CutPrefix(rest, o.op)
		if !ok {
			continue
		}
		kb, err := strconv.Atoi(v)
		if err != nil {
			return sizeTerm{}, sizeErrorf("invalid size %q: want an integer number of kB", v)
		}
		return sizeTerm{term: term, cmp: o.cmp, kb: kb}, nil
	}
	return sizeTerm{}, sizeErrorf("unknown size operator in %q: want =, <, <=, > or >=", term)
}

// checkRange returns a *dateError if the date terms among the operands of one
// AND chain cannot all hold at any single second, or a *sizeError if its size
// terms cannot all hold for any whole number of kB. Only direct leaves count:
// a term under NOT or inside an OR does not narrow the chain.
func checkRange(ops andNode) error {
	var dr dateRange
	var sr sizeRange
	for _, n := range ops {
		l, ok := n.(leaf)
		switch {
		case !ok:
		case l.date != nil:
			dr.add(*l.date)
		case l.size != nil:
			sr.add(*l.size)
		}
	}
	if dr.empty() {
		return dateErrorf("empty date range: no moment satisfies %s", strings.Join(dr.terms, " AND "))
	}
	if sr.empty() {
		return sizeErrorf("empty size range: no size satisfies %s", strings.Join(sr.terms, " AND "))
	}
	return nil
}

// sizeRange accumulates the inclusive integer bounds implied by the size
// terms of one AND chain, so that contradictory terms can be rejected.
type sizeRange struct {
	lo, hi       int // inclusive bounds in kB; valid if hasLo/hasHi
	hasLo, hasHi bool
	none         bool     // a term no int can satisfy, such as size>MaxInt
	terms        []string // the size terms seen, for error messages
}

// add narrows the range by one size term.
func (r *sizeRange) add(st sizeTerm) {
	r.terms = append(r.terms, st.term)
	switch st.cmp {
	case query.Eq:
		r.raiseLo(st.kb)
		r.lowerHi(st.kb)
	case query.Lt:
		if st.kb == math.MinInt {
			r.none = true
			return
		}
		r.lowerHi(st.kb - 1)
	case query.Le:
		r.lowerHi(st.kb)
	case query.Gt:
		if st.kb == math.MaxInt {
			r.none = true
			return
		}
		r.raiseLo(st.kb + 1)
	case query.Ge:
		r.raiseLo(st.kb)
	}
}

// raiseLo tightens the lower bound to kb if that is greater.
func (r *sizeRange) raiseLo(kb int) {
	if !r.hasLo || kb > r.lo {
		r.lo, r.hasLo = kb, true
	}
}

// lowerHi tightens the upper bound to kb if that is smaller.
func (r *sizeRange) lowerHi(kb int) {
	if !r.hasHi || kb < r.hi {
		r.hi, r.hasHi = kb, true
	}
}

// empty reports whether no whole number of kB satisfies every size term
// seen.
func (r *sizeRange) empty() bool {
	return r.none || r.hasLo && r.hasHi && r.lo > r.hi
}

// dateRange accumulates the inclusive whole-second bounds implied by the date
// terms of one AND chain, so that contradictory terms can be rejected. A day
// literal spans its first through last second; a time literal is one second.
type dateRange struct {
	lo, hi       time.Time // inclusive bounds, whole UTC seconds; valid if hasLo/hasHi
	hasLo, hasHi bool
	terms        []string // the date terms seen, for error messages
}

// add narrows the range by one date term.
func (r *dateRange) add(dt dateTerm) {
	r.terms = append(r.terms, dt.term)
	first, last := dt.lit.start, dt.lit.end.Add(-time.Second)
	switch dt.cmp {
	case query.Eq:
		r.raiseLo(first)
		r.lowerHi(last)
	case query.Lt:
		r.lowerHi(first.Add(-time.Second))
	case query.Le:
		r.lowerHi(last)
	case query.Gt:
		r.raiseLo(last.Add(time.Second))
	case query.Ge:
		r.raiseLo(first)
	}
}

// raiseLo tightens the lower bound to t if that is later.
func (r *dateRange) raiseLo(t time.Time) {
	if !r.hasLo || t.After(r.lo) {
		r.lo, r.hasLo = t, true
	}
}

// lowerHi tightens the upper bound to t if that is earlier.
func (r *dateRange) lowerHi(t time.Time) {
	if !r.hasHi || t.Before(r.hi) {
		r.hi, r.hasHi = t, true
	}
}

// empty reports whether no whole second satisfies every date term seen.
func (r *dateRange) empty() bool {
	return r.hasLo && r.hasHi && r.lo.After(r.hi)
}
