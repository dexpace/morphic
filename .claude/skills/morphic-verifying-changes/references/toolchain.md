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
  `go test ./<pkg> -covermode=set -coverprofile=f`, then
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
  on `PATH` for one run alone reruns every test that starts a child. On btrfs, committing in place
  moves `.git`'s size: clone it to the path. When the measurement needs
  `GODEBUG`, run it twice with `go clean -testcache` between and cite the second. For a cold cache,
  point `GOCACHE` at a fresh `mktemp -d`.
- **Why.** A trace cited as a reproduction worked only because the command had run before (#381),
  and a dated re-clone reran three packages only because the priming run's `PATH` differed (#1387).
- **Probe.** A test reading `PATH`, run twice under one `PATH` and once under another: the third
  run is not `(cached)`.

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

## A test opens, in its own process, what its result rests on
`test-opens-its-inputs`

- **Rule.** A test whose verdict rests on a file outside the module, or one only a child
  (`go list`, `git`) reads, opens in its own process the in-module files the child reads,
  or one that changes with them. For a dependency, call `irtest.ReadModuleFiles`;
  for a child's list, list each directory above it, and those under a pattern's root.
  `irtest.RequireOpens` holds either through the test log.
- **Why.** `readDeclarers` read library sources from the module cache, never rechecked
  by go test (#1272). Sweeping `exec(test)?\.Command`, `packages.Load` and `decls.Load` found
  `git ls-files` tests, `decls.Load`'s fixtures and a package `lintrules` names.
- **Probe.** `touch` that file and rerun: the last `ok` lacks `(cached)`. For a list, commit a new
  listed file, or package under a pattern, in a copy; clone to the primed path, `make filetimes`,
  rerun without `-count=1`.

## A test hands its child the PWD it compares paths by
`child-path-spelling`

- **Rule.** `exectest.Command` leaves `PWD` out of a child's environment. A test that compares a
  path its child printed with one it spelled passes `"PWD=" + os.Getenv("PWD")` in the child's
  `env`, as `irtest.RequireOpens` does, since from a symlinked checkout the child, with no `PWD`,
  spells the physical directory and its parent the logical one.
- **Why.** Every `RequireOpens` pin failed on a correct tree behind a symlink, which `/tmp` and
  `/var` are on macOS, and CI's real path never showed it (#1373).
- **Probe.** `ln -s` a directory above the checkout under a second name, then run the package through it
  with `go test -count=1`; a link at the module root itself also fails `internal/archtest`, whose walks do
  not descend it.

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
  Reruns never make the gate green; a `go test ./... -covermode=set -coverprofile=<file>` run and
  `go run ./cmd/morphic-covergate <file>` give the coverage verdict.
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
