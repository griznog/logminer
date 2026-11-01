// Command logmetrics loads a server access log and answers questions about it
// through an interactive prompt.
package main

import (
	"flag"
	"fmt"
	"os"

	"logmetrics/internal/logparse"
	"logmetrics/internal/repl"
)

func main() {
	path := flag.String("file", "server_log.csv", "path to the CSV server log")
	flag.Parse()

	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, "logmetrics:", err)
		os.Exit(1)
	}
}

// run loads the log at path and starts the interactive prompt.
func run(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	events, err := logparse.Parse(f)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	fmt.Printf("Loaded %d events from %s. Type \"help\" for commands.\n", len(events), path)
	return repl.Run(os.Stdin, os.Stdout, events)
}
