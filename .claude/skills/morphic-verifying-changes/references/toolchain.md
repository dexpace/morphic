# Toolchain behaviour that misleads probes

## Run a tree under the toolchain its go.mod names
`toolchain-the-tree-names`

- **Rule.** Bare `go test` keeps an installed toolchain newer than the `go` line of `go.mod`, while
  the `Makefile` pins `GOTOOLCHAIN` to that line. Before blaming a change for a failure at an old
  revision, rerun it there with `GOTOOLCHAIN=go$(go list -m -f '{{.GoVersion}}') go test ./...`.
- **Why.** At #189's first commit four tests failed under Go 1.27 and passed under the tree's
  go1.26.3, since the extension they treat as unserializable serializes under 1.27 (#1162).
- **Probe.** `go version` beside `go list -m -f '{{.GoVersion}}'`, in the worktree under test.

## Coverage counts each package's own tests
`coverage-per-package`

- **Rule.** `make coverage` profiles each package against its own tests (no `-coverpkg`). Give new
  code a test in its own package, and check a package as the `Makefile` does:
  `go test ./<pkg> -race -covermode=atomic -coverprofile=f`, then
  `go run ./cmd/morphic-covergate f`.
- **Why.** End-to-end tests exercised every branch of a schema helper and the gate still found 13
  uncovered statements (#1065).

## A re-exec'd child writes coverage only when it exits through os.Exit
`child-coverage`

- **Rule.** After changing how a re-exec'd test child starts or exits, run `make coverage`.
- **Why.** Moving an oracle from a `TestMain` hook to a `-test.run` child dropped a statement's only
  coverage (#942).

## A failed result is not cached, so the rerun names the package
`cached-rerun`

- **Rule.** Keep `make coverage` output in a file. After a lone failure, the rerun's one `ok` line
  without `(cached)` names the package that failed.
- **Why.** A go/types race failed once under contention and passed on the next run (#837).

## A cache probe holds the whole environment fixed
`cache-probe-holds-the-environment`

- **Rule.** The test cache keys a result on every environment variable the test reads, so the runs
  a cache probe compares share one `PATH`, `GODEBUG`, `GOFLAGS` and `TMPDIR`. A logging wrapper put
  on `PATH` for one run alone reruns every test that starts a child. When the measurement needs
  `GODEBUG`, run it twice with `go clean -testcache` between and cite the second. For a cold cache,
  point `GOCACHE` at a fresh `mktemp -d`; around a plant, take
  `references/toolchain.md#test-cache-keys-on-stat-and-log-order`.
- **Why.** A trace cited as a reproduction worked only because the command had run before (#381),
  and a dated re-clone reran three packages only because the priming run's `PATH` differed (#1387).
- **Probe.** A test reading `PATH`, run twice under one `PATH` and once under another: the third
  run is not `(cached)`.

## The test cache keys on an input's stat and the log's order, not its bytes
`test-cache-keys-on-stat-and-log-order`

- **Rule.** A test result is keyed on each input file's modification time and size, in the order
  the last run's log listed them, so a file `git checkout` restored reruns the next run, as
  does any run after one whose parallel tests reordered the log. Prime immediately before each
  plant, read the run after a restore's rerun by
  `references/toolchain.md#test-cache-skips-fresh-inputs`, and explain a rerun with
  `GODEBUG=gocachetest=1`.
- **Why.** A package reran on its first run after a planted comment was checked out, content as
  primed, and was `(cached)` next; three runs of one binary logged one file set in three
  orders (#1373, #1560).
- **Probe.** Prime, plant a comment in a file the package's `-test.testlogfile` lists,
  `git checkout` it, then wait 2 s: the next run is not `(cached)`, the one after is.

## The test cache saves no result while an input is under 2 s old
`test-cache-skips-fresh-inputs`

- **Rule.** `go test` saves no result from a run reading an input modified under 2 s before it, so
  after a restore each run reruns until one starts 2 s on, and the run after that one is the first
  `(cached)`. Wait 2 s after a restore before reading a cache probe.
- **Why.** Run back to back after a `git checkout`, as
  `references/toolchain.md#test-cache-keys-on-stat-and-log-order`'s Probe read at #1619's
  ef0739abe, three runs each reran, and after a 3 s wait one reran and the next was `(cached)`:
  `modTimeCutoff` in `cmd/go/internal/test/test.go` holds an input under 2 s old too new to cache
  (go1.27.3).
- **Probe.** For a fast test, `git checkout` an input its `-test.testlogfile` lists, run twice,
  `sleep 2` and run twice more: only the last is `(cached)`.

## A script started with `&` ignores an interrupt
`background-start-ignores-sigint`

- **Rule.** A script a non-interactive shell starts with `&` inherits SIGINT ignored, and bash
  cannot trap a signal it started ignoring, so an interrupt probe built that way reads alike with
  the script's `trap ... INT` removed. Start the target through GNU `env --default-signal=INT`, or a
  launcher resetting the signal, and show the probe bites by the same run with the trap removed.
- **Why.** Under `bash -c '... & wait'`, an `INT` trap never ran and a script without one survived
  the interrupt (#1382, #1604).
- **Probe.** `bash -c 'env --default-signal=INT bash -c "trap \"echo trapped\" INT; kill -INT \$\$" & wait'`
  prints `trapped`; without `env --default-signal=INT` it prints nothing.

## A test's go children never reach a go wrapper on PATH
`test-go-children-ignore-path`

- **Rule.** `go test` puts the toolchain's own `bin` directory first on the `PATH` a test sees, so
  a test's `go` children, such as the `go list -export` that `go/packages` runs, never reach a `go`
  wrapper placed earlier on `PATH`, and the wrapper's empty log reads as no child having run.
  Instrument them through `GOFLAGS=-toolexec=<logger>`, or a wrapper installed as that directory's
  `go` in a throwaway toolchain. A `git` wrapper on `PATH` works, as that directory holds no `git`.
- **Why.** A `go` wrapper on `PATH` logged no `go list -export` under `make race`, while a `git`
  wrapper beside it logged its calls (#1387).
- **Probe.** A test logging its first `PATH` entry, run as `PATH=<dir>:$PATH go test -v`, prints
  the toolchain's `bin` directory, not `<dir>`.

## Never test against a closed server's port
`closed-port`

- **Rule.** To pin how code passes a read failure through, fail a fake reader with a fixed error. To
  exercise a dial failure, dial `http://127.0.0.1:0/`.
- **Why.** Two dials of a freed port failed differently under the full suite's load, and another
  test could bind it in between (#1140).

## Temporary instrumentation needs -v
`instrument-with-v`

- **Rule.** Run a temporary `println` probe with `go test -v`: without it, passing packages print
  nothing, and only the failing package's hits appear. Attribute a hit to its test from the
  `runtime.Callers` frame in a `_test.go` file.
- **Why.** A `println` run without `-v` listed only the failing package's offenders and missed the
  `pass` fixtures (#544).

## A full disk or a removed cache reads as a failing build
`full-disk-reads-as-a-failure`

- **Rule.** Before trusting a failure, check the disk and the cache: `no space left on device`,
  `disk quota exceeded` on a temporary directory, and `could not import` naming a file under
  `GOCACHE` are the machine, not the change. Rerun with free disk and a `GOCACHE` the run owns.
- **Why.** Mutants that compile read as `[build failed]` once a shared cache filled the disk
  (#1229), 16 of 20 failed to build while other work cleared the default cache (#1333), and tests
  failed on a full shared temporary directory (#1278).
- **Probe.** `df -h "$(go env GOCACHE)"` beside the failure, and a grep of its log for
  `could not import`.

## Tell a gate failure under load from one the change caused
`gate-failure-under-load`

- **Rule.** A timeout or wall-clock bound failing in a package the change does not touch, at a load
  average above the cores the run has, is load until a rerun says otherwise. Rerun each failed
  package alone with the gate's flags, pinned with `GOMAXPROCS=<n>` beside `taskset -c` where it
  exists, then the steps the failure skipped, and report each run; a failure that holds there
  is real. Where reruns keep failing, take `references/toolchain.md#queue-gates-one-at-a-time`.
  Reruns never make the gate green; a race run with `-covermode=atomic -coverprofile=<file>` and
  `go run ./cmd/morphic-covergate <file>` still give the coverage verdict.
- **Why.** Gates failed on load while #1271 to #1274 were built, and a race run's profile gave the
  coverage verdict of a tree whose gate failed only on timeouts (#1248, #1347).
- **Probe.** `uptime` beside the failed package rerun alone.

## Run gates one at a time from a queue, not after a check
`queue-gates-one-at-a-time`

- **Rule.** Where gates under load keep failing, run them one at a time from one queue, such as a
  loop over the branches in one shell, from one checkout path with one private `GOCACHE`. Never
  start one after checking `pgrep -f 'make gate'`: it matches any process whose command line holds
  those words, and a check before a start races another start.
- **Why.** Seven branches' gates failed repeatedly on timeouts side by side, and all seven exited 0
  run one at a time from one clean checkout sharing one private `GOCACHE` (#1248, #1347).
- **Probe.** `sh -c 'sleep 3; : make gate' & pgrep -lf 'make gate'` lists a shell running no gate.

## A stale local linter fails on export data, not on the change
`stale-linter-reads-as-a-failure`

- **Rule.** A `typecheck` finding that cannot load export data, with a version "greater than
  maximum supported", is a local golangci-lint built for an older Go than the module's, not a
  defect in the change: install the version the `Makefile` pins and rerun.
  `make check-lint-version` only warns of the mismatch, and whether it should fail is #1524.
- **Why.** A local `make gate` on #1278's merged tree stopped at lint on `could not import math`,
  under golangci-lint 2.13.2 where CI pins 2.14.0 (#1286).
- **Probe.** `make check-lint-version` prints the local and pinned versions.
