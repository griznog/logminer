# Introduction

The intention of this coding challenge is to gauge your ability to program, write clean code, and discuss your work with others. Our expectation is to receive a well written solution with a clear approach that could potentially have room for improvement. With this in mind, we recommend spending no more than an hour or two on this challenge.

Some important considerations for your implementation:

* Adherence to language standards
* Well organized & documented code
* Efficient time & space complexity
* Reusable and/or extensible code

# Scenario

You are given a small sample of log of events(`server_log.csv`) for a server that allows users to upload or download files.

Using this log file as example input, write an interactive program in Go that will allow a server administrator to gather metrics from server logs in this format.

Your solution must be able to answer the following types or combinations of questions about the example log:

1. How many users accessed the server?
2. How many uploads were larger than `50kB`?
3. How many times did `jeff22` upload to the server on April 15th, 2020?

Please include a small `README.md` describing your solution and additional considerations you would make if you had more time.

# Details

The log is represented by a CSV formatted file where column data is ordered: `timestamp`, `username`, `operation`, `size`.

- The `timestamp` is recorded in the UNIX date format
- The `username` is a unique identifier for the user
- The `operation` indicates if an `upload` or `download` occurred, no other values will appear in this column
- The `size` is an integer reflecting file size in `kB`
---

# Solution

`logminer` is a Go program (standard library only) that loads a server log once and answers questions about it, either through an interactive command prompt or, for scripts and pipelines, with a single `--query`.

## Build and run

```
make build     # builds ./logminer
make test      # go vet ./... && go test ./...
make clean     # removes ./logminer and cached test results
```

`./logminer` and `go run .` are interchangeable below. The module path is still `logmetrics`; only the binary is named `logminer`.

## Usage

```
./logminer [--file server_log.csv]        # or: go run . [--file server_log.csv]
> users                                                 # Q1: distinct users           -> 6
> count op=upload AND size>50                           # Q2: uploads larger than 50 kB -> 140
> count user=jeff22 AND op=upload AND date=2020-04-15   # Q3: jeff22 uploads on Apr 15 -> 3
> users list                                            # the distinct usernames, sorted, one per line
> ops list                                              # the distinct operations, sorted, one per line
> users list op=upload AND size>50                      # who made uploads larger than 50 kB
> count date>=2020-04-13 AND date<=2020-04-15           # events from Apr 13 through Apr 15
> count 45 <= size < 100                                # events of 45 kB up to (not including) 100 kB
> select 2020-04-13 <= date <= 2020-04-15 AND op=upload # uploads from Apr 13 through Apr 15, as CSV
> select user=jeff22 AND date=2020-04-15                # jeff22's records on Apr 15, as CSV
> count (user=jeff22 OR user=Maia86) AND op=upload AND NOT size>50
                                                        # jeff22's or Maia86's uploads of at most 50 kB
> select NOT op=download AND date>=2020-04-13           # uploads from Apr 13 on, as CSV
> help
> quit
```

`users`, `ops`, `count` and `select` take an optional filter expression (none means every event). Its terms are `user=<name>`, `op=upload|download`, `size<op>N` (integer kB) and `date<op>YYYY-MM-DD`, where `<op>` is `=`, `<`, `<=`, `>` or `>=`. Size and date also take the value on the left (`50 < size`) and chained comparisons with the field in the middle: `45 <= size < 100` means `size>=45 AND size<100`, and `2020-04-13 <= date <= 2020-04-15` means `date>=2020-04-13 AND date<=2020-04-15`. Both chain operators must be `<`/`<=` or both `>`/`>=` (`100 > size >= 45` is fine, `45 < size > 10` is a syntax error), and chains on `user` or `op` are rejected. Spaces around comparison operators are optional (`45<=size<100`, `size >= 50`). Terms combine with `AND`, `OR`, `NOT` and parentheses; precedence, tightest first, is parentheses, `NOT`, `AND`, `OR`, so `NOT op=upload OR size>50 AND user=jeff22` means `(NOT op=upload) OR (size>50 AND user=jeff22)`. Keywords are case-insensitive, and parentheses need no surrounding spaces (`(user=jeff22 OR user=Maia86)AND op=upload`). `AND` is never implied: `count op=upload size>50` is an error. Syntax errors (a missing operator, unbalanced or empty parentheses, a dangling operator) print an error followed by a short guide to the expression syntax.

Dates compare whole calendar days: `date<=2020-04-15` includes all of Apr 15, `date>2020-04-15` excludes all of it. Date terms ANDed together (or chained) make a range. A bad date, an unknown date operator, or date terms in one `AND` chain that no day can satisfy (`date>2020-04-15 AND date<2020-04-10` or `2020-04-15 < date < 2020-04-10`, at any depth, including inside parentheses, an `OR` branch or `NOT (...)`) print an error followed by the date syntax help. Size terms work the same way with whole kB: a bad size, an unknown size operator, or an empty size range (`100 < size < 45`, `size>50 AND size<=50`, or `size>50 AND size<51`, since no integer lies between) print an error followed by the size syntax help. Terms on different sides of an `OR` are not combined, so `date<2020-04-10 OR date>2020-04-20` is fine.

`users` and `ops` print the number of distinct values; with `list` as the first argument they print the values themselves, sorted, one per line, or `(none)` if nothing matches. `select` prints the matching records in file order, in the input CSV format with its header, followed by `N records` (just `0 records` if nothing matches).

### Scripting

`--query` runs one command, in exactly the prompt's syntax, prints the result and exits; `--file -` reads the log from standard input (and so needs `--query`, since the prompt also reads standard input):

```
./logminer --query "count op=upload AND size>50"                     # 140
cat server_log.csv | ./logminer --file - --query "users list"        # one username per line
./logminer --query "select user=jeff22 AND date=2020-04-15" | cut -d, -f1
```

Query mode prints only the result, so it can feed other tools: no banner, prompt or load summary; `users list` and `ops list` print nothing (not `(none)`) when empty; `select` prints the CSV header and the matching records without the `N records` footer, so its output is valid CSV (just the header if nothing matches). Errors print `error: ...` and the same help text as the prompt to standard error, with nothing on standard output.

Exit codes: `0` success; `1` a bad query, or a log that cannot be read or parsed; `2` a usage error (an unknown flag, an empty `--query`, or `--file -` without `--query`). `go run` reports every non-zero exit as `1`, so use the built binary when a script needs to tell them apart.

### Line editing and history

The standard library has no readline, so the prompt itself is plain `bufio` input with no arrow-key editing or history. To get both without changing the code or adding a dependency, wrap the program with `rlwrap`:

```
rlwrap ./logminer
```

## Design

| Package | Responsibility |
| --- | --- |
| `internal/logparse` | Parses the CSV into `[]Event` (`encoding/csv`, `time.UnixDate` timestamps). Errors carry line numbers. |
| `internal/query` | Composable `Filter` functions (including `Date(cmp, y, m, d)`, of which `OnDate` is the `Eq` case, and `Size(cmp, kb)`, of which `SizeOver` is the `Gt` case, sharing one `Cmp` enum), the combinators `And`, `Or` and `Not`, plus `Count`, `Select`, `DistinctUsers`, `UniqueUsers` and `UniqueOps` (the last three share one generic set-building helper); one pass over the events per query. |
| `internal/repl` | Reads commands from an `io.Reader`, writes to an `io.Writer` (so it is easy to test). `expr.go` is a small recursive-descent parser that turns a filter expression into a tree and then a single `query.Filter` built with `And`/`Or`/`Not`. Before parsing, a pre-pass joins adjacent tokens around comparison operators (`45`, `<=`, `size` becomes `45<=size`) but never swallows parentheses or `AND`/`OR`/`NOT`. A chained comparison is desugared into an `AND` node of two ordinary field-first terms. The date and size terms of each `AND` chain are also folded into inclusive lower/upper bounds (days, or whole kB) so an empty range is reported instead of silently matching nothing. |
| `main.go` | Flag handling and log loading (a file, or standard input with `--file -`), then either one `--query` through `repl.Exec` or the interactive `repl.Run`. All in `run(args, stdin, stdout, stderr) int`, which returns the exit code, so it is tested without a process. |

New questions usually mean adding a filter constructor and one case in `parseTerm`, with no changes to the parser or the counting code.

Date and size ranges need no special filter: each term is its own `query.Date` or `query.Size` filter, ANDed like any other, and a chain is just two such terms. The range check only looks at the direct date and size operands of one `AND` chain (nested `AND` groups and chains are flattened into it); a term under `NOT` or across an `OR` does not narrow the chain, so it is never wrongly rejected. Each line is dispatched by `repl.Exec`, shared by the prompt and `--query`; `Options.Quiet` selects the script-friendly output. Errors are typed (`syntaxError`, `dateError`, `sizeError`) so `FormatError` can append the matching help text with `errors.As`; other invalid terms print just the error. The prompt prints that to standard output, `--query` to standard error. `select` re-renders records with `time.UnixDate`, so its output parses back with the same loader.

Complexity: loading is O(n); each query is O(n) time (`select` also O(m) space for the m matches), plus O(u) space for distinct values; `users list` and `ops list` add an O(u log u) sort of the u distinct values.

## Assumptions

- "Larger than 50kB" means strictly greater than 50. Sizes are whole kB, so `size>50 AND size<51` is an empty range.
- In a chain the value is on the outside and the field in the middle, read like mathematics: `45 <= size < 100` is `size>=45 AND size<100`.
- Dates are matched by calendar day in the timestamp's own time zone (all UTC in the sample) and entered as `YYYY-MM-DD`; `<`/`>` exclude the given day, `<=`/`>=` include it.
- The schema is fixed to `timestamp, username, operation, size`. A malformed row fails the load with its line number rather than being skipped.
- Usernames are matched case-sensitively.
- `AND` must be written explicitly. Implied `AND` (the original syntax) is rejected rather than guessed, since `a b OR c` is easy to misread once `OR` exists.
- `AND`, `OR` and `NOT` are reserved only as whole words; usernames appear only inside `user=...`, so they never collide. There is no quoting, so a username containing spaces or parentheses cannot be queried.

## With more time

- **Arbitrary columns:** drive the schema from the header instead of a fixed `Event` struct, with filters built from column names at runtime.
- **Type inference:** the standard library has no helper for this (`encoding/csv` returns only strings). It would be hand-built from `strconv` and `time.Parse`. It is heuristic (a zip code looks like an int) and costs an extra pass over large files, so sampling or a user-supplied schema would be preferable.
- Stream the parse and add per-user or per-day indexes for very large logs.
- Aggregates (sum/avg/min/max, group-by).
- Built-in line editing and history. `repl.Run` takes an `io.Reader`, so a library can sit behind it without touching the commands. The dependency-bearing options are:
  - [`github.com/chzyer/readline`](https://github.com/chzyer/readline): full readline behavior, history and tab completion (for example, completing command and filter names).
  - [`github.com/peterh/liner`](https://github.com/peterh/liner): a smaller library with line editing, history and completion.
  - [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term): semi-official, with basic line editing and history in `term.Terminal`, but you write more of the behavior yourself.

  I kept the solution standard-library only so it has nothing to vendor or review; `rlwrap` covers the interactive use case.
- Explicit time zone handling and a skip-and-report policy for bad rows.
- Lint and CI.
