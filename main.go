// Command logmetrics loads a server access log and answers questions about it,
// either through an interactive prompt or, with -query, one command at a time
// for use in scripts.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"logmetrics/internal/logparse"
	"logmetrics/internal/repl"
)

// Exit codes returned by run.
const (
	exitOK    = 0 // success
	exitError = 1 // bad query, or the log could not be read or parsed
	exitUsage = 2 // bad flags or flag combination
)

// stdinPath is the -file value that reads the log from standard input.
const stdinPath = "-"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run parses the command-line arguments args, loads the log and either answers
// a single -query or starts the interactive prompt on stdin. Results go to
// stdout; errors and usage messages go to stderr. It returns the exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("logmetrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("file", "server_log.csv", `path to the CSV server log, or "-" for standard input (requires -query)`)
	q := fs.String("query", "", `run one command (same syntax as the prompt, e.g. "count op=upload"), print the result and exit`)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	usage := func(msg string) int {
		fmt.Fprintln(stderr, "logmetrics:", msg)
		fs.Usage()
		return exitUsage
	}
	querySet := false
	fs.Visit(func(f *flag.Flag) { querySet = querySet || f.Name == "query" })
	switch {
	case fs.NArg() > 0:
		return usage(fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	case querySet && strings.TrimSpace(*q) == "":
		return usage("-query must not be empty")
	case *path == stdinPath && !querySet:
		return usage(`-file - reads the log from standard input, so it needs -query (the interactive prompt also reads standard input)`)
	}

	events, err := load(*path, stdin)
	if err != nil {
		fmt.Fprintln(stderr, "logmetrics:", err)
		return exitError
	}

	if querySet {
		if err := repl.Exec(stdout, events, *q, repl.Options{Quiet: true}); err != nil {
			fmt.Fprint(stderr, repl.FormatError(err))
			return exitError
		}
		return exitOK
	}

	fmt.Fprintf(stdout, "Loaded %d events from %s. Type \"help\" for commands.\n", len(events), *path)
	if err := repl.Run(stdin, stdout, events); err != nil {
		fmt.Fprintln(stderr, "logmetrics:", err)
		return exitError
	}
	return exitOK
}

// load parses the log at path, or from stdin when path is "-". Errors are
// prefixed with the source name.
func load(path string, stdin io.Reader) ([]logparse.Event, error) {
	if path == stdinPath {
		events, err := logparse.Parse(stdin)
		if err != nil {
			return nil, fmt.Errorf("stdin: %w", err)
		}
		return events, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	events, err := logparse.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return events, nil
}
