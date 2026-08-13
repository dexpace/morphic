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

# The golangci-lint release CI installs. The workflow reads it back from
# `make print-lint-version`, so the pin has one definition and no copy: without
# it the action installs whatever it resolves as latest that day, and an
# upstream release reddens main with no change to this repo.
GOLANGCI_LINT_VERSION ?= v2.12.2

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

.PHONY: gate fmt vet lint nolint-grammar nolint build coverage-count coverage \
	fuzz bench bench-smoke print-lint-version

gate: fmt vet lint nolint-grammar nolint build coverage-count coverage fuzz bench-smoke

fmt:
	@unformatted="$$(gofmt -l $$(git ls-files '*.go'))"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: not formatted:" >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

# CI installs the pinned release through golangci-lint-action; here the binary
# is whatever is on PATH. A mismatch is reported rather than fatal — a developer
# on a newer release should know their finding set is not CI's, but a tool
# version is not a reason to refuse to run the gate at all.
lint:
	@have="$$(golangci-lint version --short 2>/dev/null || true)"; \
	want="$(GOLANGCI_LINT_VERSION)"; \
	if [ "$$have" != "$${want#v}" ]; then \
		echo "warning: local golangci-lint is $${have:-absent}, CI pins $$want" >&2; \
		echo "warning:   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$$want" >&2; \
	fi
	golangci-lint run

# Both nolint checks run after lint, and in CI reach the golangci-lint the lint
# step left on PATH: they ask it which linters it is running, which only
# describes that step if it is the same build.
nolint-grammar:
	./scripts/verify-nolint-grammar.sh

nolint:
	./scripts/check-nolint-linters.sh

build:
	$(GO) build ./...

coverage-count:
	./scripts/verify-coverage-count.sh

coverage:
	./scripts/check-coverage.sh

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
