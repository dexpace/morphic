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

## GODEBUG changes the test-cache key
`godebug-cache-key`

- **Rule.** When a cache measurement needs `GODEBUG`, run it twice with `go clean -testcache`
  between and cite the second. For a cold cache, point `GOCACHE` at a fresh `mktemp -d`.
- **Why.** A trace cited as a reproduction worked only because the command had run before (#381).

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

- **Rule.** A test whose verdict rests on a file outside the module, or on one only a child
  (`go list`, `git`) reads, opens in its own process the in-module files the child reads,
  or one that changes with them. For a dependency, call `irtest.ReadModuleFiles`, held by
  `irtest.RequireOpensModuleFiles`. For a child's list, list each directory above it.
- **Why.** `readDeclarers` read library sources from the module cache, which go test never
  rechecks (#1272). Sweeping the child sites, `grep -rn --include='*.go' -E 'exec.Command'`
  and each `packages.Load` and `decls.Load`, found `ir/irtest`'s `git ls-files` tests; closed.
- **Probe.** `touch` the file the verdict rests on and rerun: the last `ok` must lack `(cached)`.
  For a list, in a scratch clone with its own `GOCACHE`, commit a file it would newly list, run
  `make filetimes`, and rerun without `-count=1`.
