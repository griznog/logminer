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
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		cmd, args := fields[0], fields[1:]
		switch cmd {
		case "quit", "exit":
			return nil
		case "help":
			fmt.Fprint(out, helpText)
		case "users", "ops", "count", "select":
			list := cmd != "count" && len(args) > 0 && args[0] == "list"
			if list {
				args = args[1:]
			}
			filter, err := parseFilters(args)
			if err != nil {
				fmt.Fprintf(out, "error: %v\n", err)
				var de *dateError
				var ze *sizeError
				var se *syntaxError
				switch {
				case errors.As(err, &de):
					fmt.Fprint(out, dateHelpText)
				case errors.As(err, &ze):
					fmt.Fprint(out, sizeHelpText)
				case errors.As(err, &se):
					fmt.Fprint(out, logicHelpText)
				}
				continue
			}
			switch {
			case cmd == "select":
				printRecords(out, query.Select(events, filter))
			case cmd == "count":
				fmt.Fprintln(out, query.Count(events, filter))
			case cmd == "users" && list:
				printList(out, query.UniqueUsers(events, filter))
			case cmd == "users":
				fmt.Fprintln(out, query.DistinctUsers(events, filter))
			case list:
				printList(out, query.UniqueOps(events, filter))
			default:
				fmt.Fprintln(out, len(query.UniqueOps(events, filter)))
			}
		default:
			fmt.Fprintf(out, "error: unknown command %q (type \"help\" for usage)\n", cmd)
		}
	}
}

// printList writes each value on its own line, or "(none)" when vals is empty
// so that an empty result is distinguishable from no output.
func printList[T ~string](out io.Writer, vals []T) {
	if len(vals) == 0 {
		fmt.Fprintln(out, "(none)")
		return
	}
	for _, v := range vals {
		fmt.Fprintln(out, v)
	}
}

// printRecords writes events in the input CSV format: a header line, one
// line per event, then a "N records" summary. With no events, only the
// summary "0 records" is written.
func printRecords(out io.Writer, events []logparse.Event) {
	if len(events) > 0 {
		fmt.Fprintln(out, recordHeader)
		for _, ev := range events {
			fmt.Fprintln(out, formatRecord(ev))
		}
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
