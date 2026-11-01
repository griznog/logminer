# Plan: Server log metrics CLI (Go)

## Context
Interview challenge: an interactive Go program that answers admin questions about `server_log.csv` (657 events, 6 users, UTC, all rows well-formed). It must answer:
1. How many users accessed the server? (6)
2. How many uploads were larger than 50kB? (140)
3. How many times did `jeff22` upload on April 15th, 2020? (3)

It should also handle combinations of these. Graded on clean, documented, efficient, extensible code (~1-2h originally; the scope below grew at the user's direction).

**Scope decision:** the schema is fixed (`timestamp, username, operation, size`). Arbitrary column counts and type inference are deferred (see Future improvements): inference adds complexity and bug surface, and needs an extra pass over large files. The program stays standard-library only.

## Design (as built)
Load once into memory as `[]Event`, then answer each query with a single filtered pass. A query is a boolean expression compiled into one `query.Filter` (`func(Event) bool`).

Layout (`go mod init logmetrics`):
- `main.go`: `-file` flag (default `server_log.csv`), load the log, start the REPL.
- `internal/logparse/`: `Event{Time, User, Op, SizeKB}`, `Parse(io.Reader)` using `encoding/csv` and `time.UnixDate`. Errors include line numbers.
- `internal/query/`
  - Leaf filters: `ByUser`, `ByOp`, `Size(cmp, kb)`, `Date(cmp, y, m, d)`, with a shared `Cmp` enum (`Eq, Lt, Le, Gt, Ge`). `SizeOver` and `OnDate` remain as thin wrappers.
  - Combinators: `And`, `Or`, `Not`.
  - Queries: `Count`, `DistinctUsers`, `UniqueUsers`, `UniqueOps`, `Select`.
- `internal/repl/`
  - `repl.go`: the command loop (`Run(io.Reader, io.Writer, events)`) and help texts.
  - `expr.go`: tokenizer, comparison-merging pre-pass, recursive-descent parser, and range validation.
  - Commands: `users [list]`, `ops [list]`, `count`, `select`, `help`, `quit`. Each takes an optional filter expression; an empty expression matches everything.

## Query language
- Leaves: `user=<name>`, `op=upload|download`, `size<op>N`, `date<op>YYYY-MM-DD`, where `<op>` is `=`, `<`, `<=`, `>`, `>=`.
- Chained ranges: `45 <= size < 100`, `2020-04-13 <= date <= 2020-04-15`. They desugar to two ANDed leaves, and both operators must point the same way.
- Operators, tightest first: parentheses, `NOT`, `AND`, `OR`. Keywords are case-insensitive. **AND is required** between terms; adjacent terms are an error.
- Dates compare whole calendar days in the timestamp's own zone. Sizes are whole kB.
- Range validation: each AND chain is checked on its own (nested ANDs flattened; terms under `OR`/`NOT` are not combined). If no day (or whole kB) satisfies the combined bounds, the query is rejected with the error and the matching date or size help text. Syntax errors print the logic help; other errors print no help.
- `select` prints matching records in the input CSV format, followed by `N records`.

## Process
Implementation was delegated in chunks to cheaper Opus subagents (parser, queries, REPL, `list`, dates and `select`, boolean expressions, chained ranges). Each chunk was reviewed and re-verified in the main session with `gofmt`, `go vet`, `go test` and shell cross-checks. Changes are committed as they are made; nothing is pushed without the user's say-so.

## Assumptions
- "Larger than 50kB" means strictly `> 50`.
- Usernames are matched case-sensitively; dates are entered as `YYYY-MM-DD`.
- A malformed row fails the load with its line number instead of being skipped.
- Complexity: O(n) load, O(n) per query, O(u log u) extra when listing distinct values.

## Future improvements (also in README)
- **Arbitrary columns:** header-driven schema instead of the fixed `Event` struct, with filters built from column names at runtime.
- **Type inference:** the stdlib has no helper (`encoding/csv` returns only strings). It would be hand-built from `strconv` and `time.Parse`; it is heuristic and costs an extra pass, so sampling or a schema file is preferable for large data.
- Built-in line editing and history (`chzyer/readline`, `peterh/liner`, `golang.org/x/term`); `rlwrap` works today.
- Streaming parse and per-user or per-day indexes; aggregates (sum/avg/min/max, group-by); explicit time zone handling; skip-and-report for bad rows; lint and CI.

## Verification
- `go vet ./... && go test ./...`
- `go run .`, enter the three challenge queries, and cross-check with shell:
  - `tail -n +2 server_log.csv | cut -d, -f2 | sort -u | wc -l`
  - `awk -F, '$3=="upload" && $4>50' server_log.csv | wc -l`
  - `grep 'Apr 15' server_log.csv | grep -c 'jeff22,upload'`
