// Package repl implements an interactive command loop for querying log events.
//
// Each command is one line: a command name followed by an optional filter
// expression, whose terms combine with AND, OR, NOT and parentheses. See the
// help text for the supported syntax.
package repl

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"logmetrics/internal/logparse"
	"logmetrics/internal/query"
)

const prompt = "> "

const helpText = `Commands:
  users [expr]           number of distinct users among matching events
  users list [expr]      sorted distinct users, one per line ("(none)" if empty)
  ops [expr]             number of distinct operations among matching events
  ops list [expr]        sorted distinct operations, one per line ("(none)" if empty)
  count [expr]           number of matching events
  select [expr]          matching records as CSV in file order, then "N records"
  help                   show this help
  quit, exit             leave the program

With no expression, every event matches. Filter terms:
  user=<name>          events by the named user (case-sensitive)
  op=<operation>       upload or download
  size<op>N            size in kB; <op> is =, <, <=, > or >=; see below
  date<op>YYYY-MM-DD   <op> is =, <, <=, > or >=; see below
  45 <= size < 100     chained comparison on size or date; see below

` + logicHelpText + `
` + sizeHelpText + `
` + dateHelpText + `
More examples:
  users
  users list
  ops list
  users list op=upload AND size>50
  count user=jeff22 AND op=upload AND date=2020-04-15
  count date>=2020-04-13 AND date<=2020-04-15
  count 45 <= size < 100
  select 2020-04-13 <= date <= 2020-04-15 AND op=upload
  select user=jeff22 AND date=2020-04-15
`

// logicHelpText explains how filter terms combine. It is part of helpText and
// is also printed after any expression syntax error.
const logicHelpText = `Terms combine with AND, OR, NOT and parentheses (keywords are case-insensitive).
Precedence, tightest first: parentheses, NOT, AND, OR. So
  NOT op=upload OR size>50 AND user=jeff22
means
  (NOT op=upload) OR (size>50 AND user=jeff22)
AND is required between terms: "op=upload size>50" is an error;
write "op=upload AND size>50".
Examples:
  count op=upload AND size>50
  count (user=jeff22 OR user=Maia86) AND op=upload AND NOT size>50
  select NOT op=download AND date>=2020-04-13
`

// dateHelpText explains the date filter syntax. It is part of helpText and is
// also printed after any date filter error.
const dateHelpText = `Date filters compare calendar days (YYYY-MM-DD) in the log's own time zone:
  date=D    on day D
  date<D    before day D (D excluded)     date<=D   on or before D (D included)
  date>D    after day D (D excluded)      date>=D   on or after D (D included)
Date terms joined by AND make a range:
  date>=2020-04-13 AND date<=2020-04-15    Apr 13 through Apr 15, inclusive
  2020-04-13 <= date <= 2020-04-15         the same range, chained
Date terms ANDed together that no day can satisfy are rejected as an error.
`

// sizeHelpText explains the size filter syntax. It is part of helpText and is
// also printed after any size filter error.
const sizeHelpText = `Size filters compare whole kB (integers):
  size=N    exactly N kB
  size<N    under N kB                    size<=N   at most N kB
  size>N    over N kB                     size>=N   at least N kB
Size terms joined by AND make a range; a chained comparison is shorthand:
  45 <= size < 100    same as size>=45 AND size<100
Both chain operators must point the same way (< and <=, or > and >=).
Size terms ANDed together that no whole kB size can satisfy are rejected as
an error, e.g. size>50 AND size<51.
`

// recordHeader is the CSV header written by "select", matching the input log.
const recordHeader = "timestamp,username,operation,size"

// Options controls how Exec formats results.
type Options struct {
	// Quiet selects script-friendly output: "select" omits its "N records"
	// footer (and always prints the header), and an empty list prints
	// nothing instead of "(none)".
	Quiet bool
}

// Run reads commands from in, writes prompts and results to out, and answers
// each command against events. It returns nil on "quit", "exit" or end of
// input. Invalid commands are reported to out and do not stop the loop; only
// an error reading from in is returned.
func Run(in io.Reader, out io.Writer, events []logparse.Event) error {
	sc := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, prompt)
		if !sc.Scan() {
			fmt.Fprintln(out)
			return sc.Err()
		}
		line := sc.Text()
		if fields := strings.Fields(line); len(fields) > 0 && (fields[0] == "quit" || fields[0] == "exit") {
			return nil
		}
		if err := Exec(out, events, line, Options{}); err != nil {
			fmt.Fprint(out, FormatError(err))
		}
	}
}

// Exec runs one command line, in the same syntax as a line typed at the
// prompt, against events and writes its result to out. A blank line, "quit"
// and "exit" do nothing. An invalid command writes nothing and returns an
// error; FormatError renders it for the user.
func Exec(out io.Writer, events []logparse.Event, line string, opts Options) error {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	cmd, args := fields[0], fields[1:]
	switch cmd {
	case "quit", "exit":
		return nil
	case "help":
		fmt.Fprint(out, helpText)
		return nil
	case "users", "ops", "count", "select":
	default:
		return fmt.Errorf("unknown command %q (type \"help\" for usage)", cmd)
	}
	list := cmd != "count" && len(args) > 0 && args[0] == "list"
	if list {
		args = args[1:]
	}
	filter, err := parseFilters(args)
	if err != nil {
		return err
	}
	switch {
	case cmd == "select":
		printRecords(out, query.Select(events, filter), opts.Quiet)
	case cmd == "count":
		fmt.Fprintln(out, query.Count(events, filter))
	case cmd == "users" && list:
		printList(out, query.UniqueUsers(events, filter), opts.Quiet)
	case cmd == "users":
		fmt.Fprintln(out, query.DistinctUsers(events, filter))
	case list:
		printList(out, query.UniqueOps(events, filter), opts.Quiet)
	default:
		fmt.Fprintln(out, len(query.UniqueOps(events, filter)))
	}
	return nil
}

// FormatError renders an error returned by Exec as an "error: ..." line,
// followed by the help text for the kind of mistake: the expression syntax
// guide for a syntax error, or the date or size filter guide for a bad date
// or size term. Other errors get no help text.
func FormatError(err error) string {
	msg := fmt.Sprintf("error: %v\n", err)
	var de *dateError
	var ze *sizeError
	var se *syntaxError
	switch {
	case errors.As(err, &de):
		msg += dateHelpText
	case errors.As(err, &ze):
		msg += sizeHelpText
	case errors.As(err, &se):
		msg += logicHelpText
	}
	return msg
}

// printList writes each value on its own line. When vals is empty it writes
// "(none)", so that an empty result is distinguishable from no output, unless
// quiet is set, in which case it writes nothing.
func printList[T ~string](out io.Writer, vals []T, quiet bool) {
	if len(vals) == 0 && !quiet {
		fmt.Fprintln(out, "(none)")
		return
	}
	for _, v := range vals {
		fmt.Fprintln(out, v)
	}
}

// printRecords writes events in the input CSV format: a header line, one
// line per event, then a "N records" summary. With no events, only the
// summary "0 records" is written. When quiet is set the output is plain CSV:
// the header is always written and the summary never is.
func printRecords(out io.Writer, events []logparse.Event, quiet bool) {
	if len(events) > 0 || quiet {
		fmt.Fprintln(out, recordHeader)
	}
	for _, ev := range events {
		fmt.Fprintln(out, formatRecord(ev))
	}
	if quiet {
		return
	}
	noun := "records"
	if len(events) == 1 {
		noun = "record"
	}
	fmt.Fprintf(out, "%d %s\n", len(events), noun)
}

// formatRecord formats ev as one line of the input CSV format, without a
// trailing newline. Fields are not quoted; the log format has no field that
// needs it.
func formatRecord(ev logparse.Event) string {
	return fmt.Sprintf("%s,%s,%s,%d", ev.Time.Format(time.UnixDate), ev.User, ev.Op, ev.SizeKB)
}
