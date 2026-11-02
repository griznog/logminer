package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testLog is a small log in the input format: 3 users, 2 operations.
const testLog = `timestamp,username,operation,size
Wed Apr 15 01:00:00 UTC 2020,jeff22,upload,60
Wed Apr 15 02:00:00 UTC 2020,jeff22,upload,10
Wed Apr 15 03:00:00 UTC 2020,jeff22,download,70
Thu Apr 16 01:00:00 UTC 2020,jeff22,upload,80
Wed Apr 15 04:00:00 UTC 2020,sarah94,upload,50
Tue Apr 14 05:00:00 UTC 2020,Maia86,download,51
`

func TestRun(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "log.csv")
	if err := os.WriteFile(logFile, []byte(testLog), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		args     []string
		stdin    string
		wantCode int
		wantOut  string // exact stdout
		wantErr  string // substring of stderr; "" means stderr must be empty
	}{
		{"count stdin", []string{"--file", "-", "--query", "count op=upload AND size>50"}, testLog, 0, "2\n", ""},
		{"users stdin", []string{"--file", "-", "--query", "users"}, testLog, 0, "3\n", ""},
		{"users list stdin", []string{"--file", "-", "--query", "users list"}, testLog, 0, "Maia86\njeff22\nsarah94\n", ""},
		{"users list empty", []string{"--file", "-", "--query", "users list user=nobody"}, testLog, 0, "", ""},
		{"ops stdin", []string{"--file", "-", "--query", "ops"}, testLog, 0, "2\n", ""},
		{"ops list stdin", []string{"-file", "-", "-query", "ops list"}, testLog, 0, "download\nupload\n", ""},
		{
			"select stdin", []string{"--file", "-", "--query", "select user=jeff22 AND date=2020-04-15"}, testLog, 0,
			"timestamp,username,operation,size\n" +
				"Wed Apr 15 01:00:00 UTC 2020,jeff22,upload,60\n" +
				"Wed Apr 15 02:00:00 UTC 2020,jeff22,upload,10\n" +
				"Wed Apr 15 03:00:00 UTC 2020,jeff22,download,70\n",
			"",
		},
		{"select empty", []string{"--file", "-", "--query", "select user=nobody"}, testLog, 0, "timestamp,username,operation,size\n", ""},
		{"query from file", []string{"--file", logFile, "--query", "count user=jeff22 AND op=upload AND date=2020-04-15"}, "", 0, "2\n", ""},
		{"quit is a no-op", []string{"--file", logFile, "--query", "quit"}, "", 0, "", ""},
		{"bad query", []string{"--file", "-", "--query", "count op=bogus"}, testLog, 1, "", "error: invalid op \"bogus\""},
		{"syntax error", []string{"--file", "-", "--query", "count op=upload size>50"}, testLog, 1, "", "error: "},
		{"date error", []string{"--file", "-", "--query", "count date=4/15"}, testLog, 1, "", "error: invalid date"},
		{"time range", []string{"--file", "-", "--query", "count 2020-04-15T02:00 <= date < 2020-04-15T04:00"}, testLog, 0, "2\n", ""},
		{"time empty range", []string{"--file", "-", "--query", "count date>2020-04-15T10:00:00 AND date<2020-04-15T10:00:01"}, testLog, 1, "", "error: empty date range: no moment satisfies"},
		{"time error", []string{"--file", "-", "--query", "count date>=2020-04-15T25:00"}, testLog, 1, "", "error: invalid date \"2020-04-15T25:00\""},
		{"unknown command", []string{"--file", "-", "--query", "frob"}, testLog, 1, "", "error: unknown command \"frob\""},
		{"malformed csv", []string{"--file", "-", "--query", "count"}, "timestamp,username,operation,size\nnot,a,valid,row\n", 1, "", "logminer: stdin:"},
		{"missing file", []string{"--file", filepath.Join(t.TempDir(), "nope.csv"), "--query", "count"}, "", 1, "", "logminer: open"},
		{"stdin without query", []string{"--file", "-"}, testLog, 2, "", "needs -query"},
		{"empty query", []string{"--file", logFile, "--query", ""}, "", 2, "", "-query must not be empty"},
		{"blank query", []string{"--file", logFile, "--query", "   "}, "", 2, "", "-query must not be empty"},
		{"unknown flag", []string{"--bogus"}, "", 2, "", "flag provided but not defined: -bogus"},
		{"extra argument", []string{"--file", logFile, "--query", "count", "extra"}, "", 2, "", "unexpected argument \"extra\""},
		{
			"interactive", []string{"--file", logFile}, "count\nusers list user=nobody\nquit\n", 0,
			"Loaded 6 events from " + logFile + ". Type \"help\" for commands.\n> 6\n> (none)\n> ",
			"",
		},
		{
			"interactive error goes to stdout", []string{"--file", logFile}, "count op=bogus\n", 0,
			"Loaded 6 events from " + logFile + ". Type \"help\" for commands.\n" +
				"> error: invalid op \"bogus\": want upload or download\n> \n",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, strings.NewReader(tt.stdin), &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr %q)", code, tt.wantCode, stderr.String())
			}
			if got := stdout.String(); got != tt.wantOut {
				t.Errorf("stdout = %q, want %q", got, tt.wantOut)
			}
			gotErr := stderr.String()
			switch {
			case tt.wantErr == "" && gotErr != "":
				t.Errorf("stderr = %q, want empty", gotErr)
			case !strings.Contains(gotErr, tt.wantErr):
				t.Errorf("stderr = %q, want it to contain %q", gotErr, tt.wantErr)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--file", "-", "--query", "help"}, strings.NewReader(testLog), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "Commands:\n") {
		t.Errorf("stdout = %q, want the help text", stdout.String())
	}
}

// TestRunErrorHelp checks that a query error prints the help text matching
// its kind to stderr, as the interactive prompt does.
func TestRunErrorHelp(t *testing.T) {
	tests := []struct {
		query string
		help  string // a line from the expected help text
	}{
		{"count op=upload size>50", "Terms combine with AND, OR, NOT and parentheses"},
		{"count date=4/15", "Date filters take a day"},
		{"count date>=2020-04-15T25:00", "Date filters take a day"},
		{"count size>abc", "Size filters compare whole kB"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"--file", "-", "--query", tt.query}, strings.NewReader(testLog), &stdout, &stderr); code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if !strings.HasPrefix(stderr.String(), "error: ") || !strings.Contains(stderr.String(), tt.help) {
				t.Errorf("stderr = %q, want an error line and %q", stderr.String(), tt.help)
			}
		})
	}
}

// TestRunChallengeAnswers answers the three challenge questions through
// -query against the real log; see query.TestChallengeAnswers for how the
// expected values were computed.
func TestRunChallengeAnswers(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{"users", "6\n"},
		{"count op=upload AND size>50", "140\n"},
		{"count user=jeff22 AND op=upload AND date=2020-04-15", "3\n"},
		// See internal/repl.TestRunTimeQueries for the shell cross-check.
		{"count 2020-04-15T09:00 <= date < 2020-04-15T17:00", "15\n"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{"--file", "server_log.csv", "--query", tt.query}, strings.NewReader(""), &stdout, &stderr); code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr.String())
			}
			if got := stdout.String(); got != tt.want {
				t.Errorf("stdout = %q, want %q", got, tt.want)
			}
		})
	}
}
