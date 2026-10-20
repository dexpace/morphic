# The gate, defined once.
#
# .github/workflows/gate.yml runs these targets, so `make gate` and the CI job
# are the same commands by construction rather than by two lists that agree until
# somebody edits one. Add a check here and the workflow gains a step that calls
# it; there is no second place to keep in step.
#
#   make            the whole gate, in CI order
#   make coverage   the suite once, under -race, at exactly 100% coverage
#   make bench      benchmark timings (the gate only smoke-runs them)
#   make fuzz FUZZTIME=5m    a longer search than the gate's

GO ?= go

# The Go toolchain the gate runs on, read from go.mod so the pin has one
# definition and no copy — the same discipline GOLANGCI_LINT_VERSION gets below,
# and for a sharper reason: the gate is only "the commands CI runs" if it runs
# them on the toolchain CI uses. golangci-lint bundles a staticcheck that builds
# its own IR of every package it loads, the standard library included, so a
# stdlib newer than that release knows panics the linter before it reaches a
# line of this repo. Exported, because the scripts below and golangci-lint each
# invoke go themselves, and the linter reads the stdlib through `go list`.
#
# Moving this means moving GOLANGCI_LINT_VERSION with it, to a release whose
# staticcheck knows the new stdlib; moving golang.org/x/tools in go.mod to a
# release that reads the new toolchain's export data, which internal/archtest
# and internal/decls type-check packages through; and rewriting the two rows of
# rawDivergences that pin an encoding/json escape (#431). Bump the four
# together or the gate fails on something other than the change under test.
GO_VERSION := $(shell sed -n 's/^go //p' go.mod)
ifeq ($(GO_VERSION),)
$(error no go directive found in go.mod; the toolchain pin has nothing to read)
endif

GOTOOLCHAIN ?= go$(GO_VERSION)
export GOTOOLCHAIN

# A GOTOOLCHAIN already set in the environment wins, as an override should. It is
# reported rather than refused, exactly as a local golangci-lint of the wrong
# version is: a developer running the gate on another toolchain should know the
# result is not CI's, and a toolchain setting is not a reason to decline to run
# the gate at all.
ifneq ($(GOTOOLCHAIN),go$(GO_VERSION))
$(warning warning: GOTOOLCHAIN is $(GOTOOLCHAIN), go.mod pins go$(GO_VERSION))
$(warning warning:   unset GOTOOLCHAIN to run the gate as CI runs it)
endif

# The gate's go commands run with -trimpath, added to whatever GOFLAGS the
# caller set. Without it the go command keys this module's packages by the
# absolute directory they were built in, so two checkouts of one commit share
# nothing in the build cache, and the heaviest packages here compile to tens of
# megabytes apiece. Exported for the same reason GOTOOLCHAIN is: the scripts and
# golangci-lint invoke go themselves. CI's make steps get it too, so a test that
# finds files through its own source path, which -trimpath erases, fails there.
GOFLAGS := $(strip $(GOFLAGS) -trimpath)
export GOFLAGS

# The golangci-lint release CI installs. The workflow reads it back from
# `make print-lint-version`, so the pin has one definition and no copy: without
# it the action installs whatever it resolves as latest that day, and an
# upstream release reddens main with no change to this repo.
GOLANGCI_LINT_VERSION ?= v2.14.0

# Per-target fuzz budget, bounded on purpose. The gate's job is to keep every
# target executable and to search a little on every change; a campaign is what
# `make fuzz FUZZTIME=5m` is for.
FUZZTIME ?= 10s

.DEFAULT_GOAL := gate

# The gate runs one check at a time even under `make -j`. Prerequisites of a
# single target are otherwise eligible to run concurrently, which would both lose
# the CI order this file exists to mirror and let `fuzz` write a reproducer into
# testdata/ while `coverage` is running `go test ./...` over the same tree.
.NOTPARALLEL:

.PHONY: gate fmt vet lint lint-planted check-lint-version build coverage \
	fuzz bench bench-smoke print-lint-version print-go-version

gate: fmt vet lint lint-planted build coverage fuzz bench-smoke

fmt:
	@unformatted="$$(gofmt -l $$(git ls-files '*.go'))"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: not formatted:" >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

lint: check-lint-version
	golangci-lint run

# The rules in .golangci.yml that a clean tree cannot prove, held to the findings
# planted for them; scripts/lint-planted.sh says how. In CI it runs after the
# lint action, which leaves its golangci-lint on PATH for the steps after it.
lint-planted: check-lint-version
	./scripts/lint-planted.sh

# CI installs the pinned release through golangci-lint-action; here the binary
# is whatever is on PATH. A mismatch is reported rather than fatal — a developer
# on a newer release should know their finding set is not CI's, but a tool
# version is not a reason to refuse to run the gate at all.
check-lint-version:
	@have="$$(golangci-lint version --short 2>/dev/null || true)"; \
	want="$(GOLANGCI_LINT_VERSION)"; \
	if [ "$$have" != "$${want#v}" ]; then \
		echo "warning: local golangci-lint is $${have:-absent}, CI pins $$want" >&2; \
		echo "warning:   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$$want" >&2; \
	fi

# The build runs for the host and again for ports CI never runs on, since an
# API one port lacks fails only that port's build: Windows, and the two wasm
# ports, whose syscall package is the narrowest the module builds against.
build:
	$(GO) build ./...
	GOOS=windows GOARCH=amd64 $(GO) build ./...
	GOOS=js GOARCH=wasm $(GO) build ./...
	GOOS=wasip1 GOARCH=wasm $(GO) build ./...

# The suite runs once, under the race detector, and cmd/morphic-covergate judges
# its profile statement by statement; its doc says why go test's percentage is
# not enough. -race rides along rather than taking a run of its own: both want
# the same execution of the same tests, and the profile is identical either way.
#
# -timeout is explicit so that a test that never returns, as one would if the
# third-party resolver hung on a document the compiler no longer refuses, ends
# in a stack dump naming the stuck goroutine. 300s clears the slowest package
# under -race by more than an order of magnitude.
#
# The profile path is minted per run: go test truncates it at the start and
# appends each package as it finishes, so two runs sharing one would each judge
# blocks partly the other's. COVER_FILE names a profile to keep instead. A failed
# mktemp ends the recipe at once: recipe lines run without -e, so otherwise the
# whole suite would run before anything noticed.
coverage:
	@profile="$${COVER_FILE:-}"; \
	if [ -z "$$profile" ]; then \
		profile="$$(mktemp "$${TMPDIR:-/tmp}/morphic-cover.XXXXXX")" || exit 1; \
		trap 'rm -f "$$profile"' EXIT; \
	fi; \
	$(GO) test ./... -race -timeout 300s -covermode=atomic -coverprofile="$$profile" && \
	$(GO) run ./cmd/morphic-covergate "$$profile"

fuzz:
	./scripts/fuzz.sh $(FUZZTIME)

bench:
	./scripts/bench.sh

# One iteration of every benchmark: enough to prove they still build, still find
# their fixtures and still complete, too few to mean anything as a timing.
bench-smoke:
	./scripts/bench.sh 1x

print-lint-version:
	@echo $(GOLANGCI_LINT_VERSION)

print-go-version:
	@echo $(GO_VERSION)
