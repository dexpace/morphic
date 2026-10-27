# Toolchain behaviour that misleads probes

## Coverage counts each package's own tests
`coverage-per-package`

- **Rule.** `make coverage` profiles each package against its own tests (no `-coverpkg`). Give new
  code a test in its own package, and check a package with
  `go test ./pkg -covermode=atomic -coverprofile=f` and `go run ./cmd/morphic-covergate f`.
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

## A cancellation claim needs a gap probe
`cancellation-gaps`

- **Rule.** For "every walk asks the context", wrap the context so `Err` records the longest gaps
  between asks over one whole compile of a large spec. Pin each new ask with a test that counts asks
  through `ctxtest.Countdown` and cuts partway.
- **Why.** An allocation-bounded test passed while a lazy index walked a million nodes without
  asking (#1131).

## A build in the worktree can dirty it
`build-dirties-tree`

- **Rule.** Build a command to `-o /dev/null` or a temporary path, and check
  `git status --untracked-files=all` before committing.
- **Why.** `go build ./cmd/morphic` writes the binary into the working directory, one `git add -A`
  from a commit (#828).
