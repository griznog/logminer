# Plan: Server log metrics CLI (Go)

## Context
Interview challenge: an interactive Go program that answers admin questions about `server_log.csv` (657 events, 6 users, UTC, all rows well-formed). It must answer:
1. How many users accessed the server?
2. How many uploads were larger than 50kB?
3. How many times did `jeff22` upload on April 15th, 2020?

It should also handle combinations of these. Graded on clean, documented, efficient, extensible code (~1-2h).

**Scope decision:** the schema is fixed (`timestamp, username, operation, size`). Arbitrary column counts and type inference are deliberately deferred (see Future improvements): inference adds complexity and bug surface, and needs an extra pass over large files.

## Design
Load once into memory as `[]Event`, then answer each query with a single filtered pass. Queries are composable filters rather than three hardcoded functions, which covers "combinations" and is the extensibility story.

Layout (stdlib only, `go mod init logmetrics`):
- `main.go`: `-file` flag (default `server_log.csv`), load the log, start the REPL.
- `internal/logparse/`
  - `Event{Time time.Time; User string; Op Operation; SizeKB int}`; `Operation` is `upload|download`.
  - `Parse(io.Reader) ([]Event, error)` using `encoding/csv`: skip header, `time.Parse(time.UnixDate, ...)`, `strconv.Atoi` for size, reject unknown operations. Errors include line numbers.
- `internal/query/`
  - `Filter = func(Event) bool`; constructors `ByUser`, `ByOp`, `SizeOver(kb)`, `OnDate(y, m, d)`.
  - `Count(events, ...Filter) int`, `DistinctUsers(events, ...Filter) int`.
- `internal/repl/`: reads from `io.Reader`, writes to `io.Writer` (testable). Commands:
  - `users [filters]`: distinct user count
  - `count [filters]`: matching event count
  - filters: `user=<name>`, `op=upload|download`, `size>50`, `date=YYYY-MM-DD`
  - `help`, `quit`
  - Q1 `users`; Q2 `count op=upload size>50`; Q3 `count user=jeff22 op=upload date=2020-04-15`.
- Tests (table-driven): parser (header, bad timestamp, bad size, bad op, wrong field count), each filter, REPL commands and error messages, plus an integration test on `server_log.csv` asserting the three answers.
- `README.md`: rewrite to describe the solution, usage, assumptions, and the Future improvements below. The zip is a duplicate of the inputs; ask whether to remove or gitignore it.

## Execution
Implementation is delegated to cheaper Opus subagents (`model: "opus"`), each with a self-contained prompt; I review and verify in the main session.
1. Agent A: `go mod init`, `internal/logparse` plus tests.
2. Agent B (after A, since it needs `Event`): `internal/query` plus tests.
3. Agent C (after B): `internal/repl`, `main.go`, and the integration test.
4. Main session: review the diffs, run `go vet` and `go test`, run the Verification commands, then write the README.

## Assumptions to document
- "Larger than 50kB" means strictly `> 50` (size is an integer in kB).
- Date match uses the timestamp's own zone (all UTC in the sample).
- Date input format is `YYYY-MM-DD`.
- Complexity: O(n) load, O(n) per query, O(u) space for distinct users.

## Future improvements (for README)
- **Arbitrary columns:** header-driven schema instead of the fixed `Event` struct; filters built from column names at runtime.
- **Type inference:** the stdlib has no such helper (`encoding/csv` returns only strings). It would be hand-built from `strconv.ParseInt`/`ParseFloat`/`ParseBool` and `time.Parse` over candidate layouts. It is heuristic (zip codes become ints) and costs an extra pass, so sampling or a user-supplied schema file would be preferable for large data.
- Streaming parse and per-user/per-day indexes for very large logs.
- Aggregates (sum/avg/min/max, group-by), `AND`/`OR`, and ranges.
- readline/history in the REPL, timezone handling, malformed-row policy (skip and report), lint/CI.

## Verification
- `go vet ./... && go test ./...`
- `go run .`, enter the three queries, and cross-check with shell:
  - `tail -n +2 server_log.csv | cut -d, -f2 | sort -u | wc -l`
  - `awk -F, '$3=="upload" && $4>50' server_log.csv | wc -l`
  - `grep 'Apr 15' server_log.csv | grep -c 'jeff22,upload'`
