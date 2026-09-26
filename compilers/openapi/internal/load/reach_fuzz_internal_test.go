package load

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// update rewrites testdata/reach_fuzz_crashes.golden instead of comparing
// against it, the same flag name and meaning ir/irtest's golden helper uses
// for IR documents (this package cannot import ir/irtest itself: it sits
// below ir/irtest in the layering, and the golden here is a list of indices,
// not an ir.Document).
var update = flag.Bool("update", false, "rewrite testdata/reach_fuzz_crashes.golden instead of comparing")

// reachFuzzOracleEnv, when set in the environment, tells this test binary to
// act as the crash oracle subprocess instead of running the test suite: read
// the spec file it names, run ONLY the raw library's own resolver over it with
// a bounded stack, and exit rather than crash the parent test process the way
// a real, unrecoverable stack overflow would. TestMain intercepts this before
// testing.Main ever runs — the same self-exec pattern the standard library's
// own os/exec tests use for a "helper process" — because the crash this file
// looks for cannot be recovered in-process (bounded-recursion styleguide rule:
// Go does not recover a stack-exhaustion fatal error, and debug.SetMaxStack is
// process-wide, so it must be set in a process this test can afford to lose).
//
// Only regenerateReachFuzzGolden (run under -update) still spawns this
// subprocess; the normal comparison path never does (see
// TestReachFuzz_RefusesEveryCycleTheResolverRecursesInto's own doc comment
// for why).
const reachFuzzOracleEnv = "MORPHIC_REACH_FUZZ_ORACLE_SPEC"

// TestMain lets this package act as its own crash-oracle subprocess. Every
// other test in the package is unaffected: the environment variable is unset
// unless reachFuzzSubprocessCrashed sets it for a child process it spawns.
func TestMain(m *testing.M) {
	if specPath := os.Getenv(reachFuzzOracleEnv); specPath != "" {
		os.Exit(runReachFuzzOracle(specPath))
	}
	os.Exit(m.Run())
}

// runReachFuzzOracle runs the raw library's resolver over the spec at
// specPath and returns the process exit code: 0 if the resolver returns at
// all (whether or not it resolves everything), nonzero for an I/O or parse
// failure this oracle cannot drive into the resolver. A genuine stack
// overflow never returns from this function — the Go runtime terminates the
// process with a fatal error and exit status 2 first, which is the signal
// reachFuzzSubprocessCrashed reads.
func runReachFuzzOracle(specPath string) int {
	debug.SetMaxStack(64 << 20)
	data, err := os.ReadFile(specPath)
	if err != nil {
		return 3
	}
	ctx := context.Background()
	doc, _, err := soa.Unmarshal(ctx, bytes.NewReader(data))
	if err != nil {
		return 0 // not a shape this oracle can drive into the resolver; not a crash either
	}
	_, _ = doc.ResolveAllReferences(ctx, soa.ResolveAllOptions{OpenAPILocation: specPath, DisableExternalRefs: true})
	return 0
}

// reachFuzzSubprocessCrashed re-execs this test binary as the oracle
// subprocess over specPath and reports whether it crashed: exit status 2 is
// what a Go "fatal error: stack overflow" terminates with.
func reachFuzzSubprocessCrashed(ctx context.Context, t *testing.T, specPath string) bool {
	t.Helper()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), reachFuzzOracleEnv+"="+specPath)
	err := cmd.Run()
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if !assert.ErrorAs(t, err, &exitErr, "the oracle subprocess must exit rather than fail to start") {
		return false
	}
	return exitErr.ExitCode() == 2
}

// reachFuzzRefs, reachFuzzIDs and reachFuzzAnchors are the fixed vocabularies
// the grammar below draws $ref/$id/$anchor values from, ported from the
// throwaway Python script that found GitHub #526 and #546 by fuzzing, so the
// same adversarial $ref/$anchor/$id/$defs soup runs as a committed regression
// instead of only a one-off script: random mixes of "#", "#/" and
// $defs-relative pointers, cross-component references, and relative/absolute
// $id values.
var (
	reachFuzzRefs = []string{
		"#a", "#b", "#/$defs/m", "#/$defs/n", "#/$defs/m/properties/p",
		"#/components/schemas/A", "#/components/schemas/B", "#/components/schemas/C",
		"#/components/schemas/A/$defs/m", "#/components/schemas/B/$defs/n",
		"#/components/schemas/A/properties/p", "#/components/schemas/B/properties/q",
		"https://x.test/a", "https://x.test/a#a", "https://x.test/b#/$defs/m", "a.json", "#/x-s", "#/x-s/$defs/m",
	}
	reachFuzzIDs     = []string{"https://x.test/a", "https://x.test/b", "a.json"}
	reachFuzzAnchors = []string{"a", "b"}
)

// reachFuzzSchema is one node of the grammar's tree: a subset of
// $ref/type/$anchor/$id/$defs/properties chosen at random, kept as a struct
// rather than a generic map so writeJSON below controls key order deterministically
// (a map has none).
type reachFuzzSchema struct {
	ref, typ, anchor, id              string
	hasRef, hasType, hasAnchor, hasID bool
	defs, props                       map[string]*reachFuzzSchema
	defOrder, propOrder               []string
}

// genFuzzSchema generates one random schema node. depth is an explicit,
// decrementing bound on the recursion (styleguide bounded-recursion rule):
// callers below pass 2 for a component and 1 for the document-level "x-s"
// extension, so nesting stops well short of anything pathological.
func genFuzzSchema(rng *rand.Rand, depth int) *reachFuzzSchema {
	s := &reachFuzzSchema{}
	if rng.Float64() < 0.55 {
		s.ref, s.hasRef = reachFuzzRefs[rng.Intn(len(reachFuzzRefs))], true
	} else {
		s.typ, s.hasType = []string{"object", "string"}[rng.Intn(2)], true
	}
	if rng.Float64() < 0.3 {
		s.anchor, s.hasAnchor = reachFuzzAnchors[rng.Intn(len(reachFuzzAnchors))], true
	}
	if rng.Float64() < 0.15 {
		s.id, s.hasID = reachFuzzIDs[rng.Intn(len(reachFuzzIDs))], true
	}
	if depth > 0 && rng.Float64() < 0.45 {
		s.defOrder = fuzzSample(rng, []string{"m", "n"})
		s.defs = genFuzzChildren(rng, depth, s.defOrder)
	}
	if depth > 0 && rng.Float64() < 0.35 {
		s.propOrder = fuzzSample(rng, []string{"p", "q"})
		s.props = genFuzzChildren(rng, depth, s.propOrder)
	}
	return s
}

func genFuzzChildren(rng *rand.Rand, depth int, keys []string) map[string]*reachFuzzSchema {
	out := make(map[string]*reachFuzzSchema, len(keys))
	for _, k := range keys {
		out[k] = genFuzzSchema(rng, depth-1)
	}
	return out
}

// fuzzSample returns a random-sized, randomly ordered subset of pool with no
// repeats — a size from 1 to len(pool), inclusive.
func fuzzSample(rng *rand.Rand, pool []string) []string {
	n := 1 + rng.Intn(len(pool))
	picked := append([]string(nil), pool...)
	rng.Shuffle(len(picked), func(i, j int) { picked[i], picked[j] = picked[j], picked[i] })
	return picked[:n]
}

// writeJSON renders s as a flow-style JSON object to embed inline in the
// surrounding YAML; every value in this grammar's fixed vocabularies is plain
// ASCII with no character %q would escape unexpectedly, so this needs no
// general JSON encoder.
func (s *reachFuzzSchema) writeJSON(sb *bytes.Buffer) {
	sb.WriteByte('{')
	first := true
	field := func(key, value string) {
		if !first {
			sb.WriteString(", ")
		}
		first = false
		fmt.Fprintf(sb, "%q: %q", key, value)
	}
	if s.hasRef {
		field("$ref", s.ref)
	}
	if s.hasType {
		field("type", s.typ)
	}
	if s.hasAnchor {
		field("$anchor", s.anchor)
	}
	if s.hasID {
		field("$id", s.id)
	}
	s.writeChildren(sb, &first, "$defs", s.defOrder, s.defs)
	s.writeChildren(sb, &first, "properties", s.propOrder, s.props)
	sb.WriteByte('}')
}

func (s *reachFuzzSchema) writeChildren(sb *bytes.Buffer, first *bool, key string, order []string, children map[string]*reachFuzzSchema) {
	if children == nil {
		return
	}
	if !*first {
		sb.WriteString(", ")
	}
	*first = false
	fmt.Fprintf(sb, "%q: {", key)
	for i, k := range order {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(sb, "%q: ", k)
		children[k].writeJSON(sb)
	}
	sb.WriteByte('}')
}

// genFuzzDoc generates one random document: an OpenAPI 3.1 skeleton, an
// optional "x-s" extension schema, and one to three components named from
// {A, B, C}, each an independently generated reachFuzzSchema.
func genFuzzDoc(rng *rand.Rand) string {
	var sb bytes.Buffer
	sb.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n")
	if rng.Float64() < 0.2 {
		sb.WriteString("x-s: ")
		genFuzzSchema(rng, 1).writeJSON(&sb)
		sb.WriteByte('\n')
	}
	sb.WriteString("components:\n  schemas:\n")
	for _, k := range fuzzSample(rng, []string{"A", "B", "C"}) {
		sb.WriteString("    " + k + ": ")
		genFuzzSchema(rng, 2).writeJSON(&sb)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// reachFuzzSeed and reachFuzzN fix the grammar's sample: the same seed and
// count genReachFuzzDocs and (formerly) the golden's own regeneration always
// use, so an index in testdata/reach_fuzz_crashes.golden names the same
// document whether it was written under -update or read back by a normal
// run. math/rand's v1 source — used here, not math/rand/v2 — is documented to
// produce the same sequence for a given seed across Go versions, so this
// reproduces the golden's own inputs exactly without the golden needing to
// store the documents themselves.
const (
	reachFuzzSeed = 1
	reachFuzzN    = 200
)

// reachFuzzGoldenPath is testdata/, which go tool ignores as a build package
// on its own, per Go convention for fixture directories.
const reachFuzzGoldenPath = "testdata/reach_fuzz_crashes.golden"

const reachFuzzGoldenHeader = `# Generated by:
#   go test ./compilers/openapi/internal/load -run TestReachFuzz_RefusesEveryCycleTheResolverRecursesInto -update
#
# One index per line: which of reachFuzzN documents genReachFuzzDocs
# generates, in order, from rand.New(rand.NewSource(reachFuzzSeed)), crash the
# vendored speakeasy-api/openapi resolver with an unrecoverable stack
# overflow. math/rand's v1 source (used here, not math/rand/v2) is documented
# to produce the same sequence for a given seed across Go versions, so
# genReachFuzzDocs reproduces these exact documents on every run without this
# file needing to store them itself.
#
# A dependency bump to speakeasy-api/openapi that changes the resolver's own
# behavior can make this file stale. Regenerate it with -update. If that
# finds a document which now crashes the resolver but
# TestReachFuzz_RefusesEveryCycleTheResolverRecursesInto does not refuse,
# that is a real finding to fix, not a golden to accept as given.
`

// genReachFuzzDocs deterministically generates reachFuzzN documents from the
// grammar above, seeded with reachFuzzSeed. Both the golden's regeneration
// path (-update) and the normal comparison path call this, so an index
// always names the same document in either.
func genReachFuzzDocs() []string {
	rng := rand.New(rand.NewSource(reachFuzzSeed))
	docs := make([]string, reachFuzzN)
	for i := range docs {
		docs[i] = genFuzzDoc(rng)
	}
	return docs
}

// loadRefusedAsCycle runs spec through the public Load entry point and
// reports whether it came back refused as a cyclic reference — end to end,
// regardless of which stage refuses it. The fuzz grammar below generates
// plenty of plain root-pointer self-references (a component whose own $ref
// names itself with no $anchor/$id/$defs involved at all), which the
// pre-parse pointer-chain scan refuses on its own before chainCycle's gate
// would even run reach; checking chainCycle in isolation here would fail this
// test on a gate doing exactly what TestChainCycle_GateSkipsPlainPointerDocuments
// says it should.
func loadRefusedAsCycle(t *testing.T, spec string) bool {
	t.Helper()
	doc, diags, err := Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(spec)}, Options{})
	require.NoError(t, err, "a reference cycle is a spec problem, not a Go error")
	return doc == nil && countErrorsAt(diags, diag.CyclicRef) > 0
}

// regenerateReachFuzzGolden runs the subprocess crash oracle over every
// document in docs and rewrites goldenPath with the indices that crash. It is
// the only code path left in this file that still spawns a subprocess per
// document, which is why the real sweep (reachFuzzN documents, written to
// reachFuzzGoldenPath) runs only under -update instead of on every test run:
// race instrumentation multiplies each subprocess's own startup cost, and 200
// of them under -race do not fit a normal test run's time budget.
// TestRegenerateAndReadReachFuzzGolden_RoundTrip below calls this directly
// (bypassing the -update flag) against a two-document set and a scratch
// path, so every line here still runs — and is covered — on every ordinary
// test invocation without paying the 200-subprocess cost.
func regenerateReachFuzzGolden(t *testing.T, docs []string, goldenPath string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	dir := t.TempDir()

	var crashed []int
	for i, spec := range docs {
		specPath := filepath.Join(dir, fmt.Sprintf("f%03d.yaml", i))
		require.NoError(t, os.WriteFile(specPath, []byte(spec), 0o600))
		if reachFuzzSubprocessCrashed(ctx, t, specPath) {
			crashed = append(crashed, i)
		}
		require.NoError(t, ctx.Err(), "regenerating the golden ran out of time after %d/%d documents", i+1, len(docs))
	}
	require.NotEmpty(t, crashed, "regenerated golden would be empty: the grammar or seed no longer reaches a crash")

	var sb strings.Builder
	sb.WriteString(reachFuzzGoldenHeader)
	for _, i := range crashed {
		fmt.Fprintf(&sb, "%d\n", i)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
	require.NoError(t, os.WriteFile(goldenPath, []byte(sb.String()), 0o644))
	t.Logf("wrote %d crash indices (of %d documents) to %s", len(crashed), len(docs), goldenPath)
}

// readReachFuzzGolden reads and parses the golden file at goldenPath,
// skipping "#"-prefixed comment lines and blank lines. It requires the result
// to be non-empty and every index in range: a golden truncated to nothing, or
// corrupted past parseable integers, fails loudly here rather than leaving
// the comparison below nothing to check and so passing vacuously.
func readReachFuzzGolden(t *testing.T, goldenPath string) []int {
	t.Helper()
	raw, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "read golden %s (run with -update to create it)", goldenPath)

	var indices []int
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i, err := strconv.Atoi(line)
		require.NoError(t, err, "golden %s: %q is not an index", goldenPath, line)
		require.True(t, i >= 0 && i < reachFuzzN,
			"golden %s: index %d out of range [0,%d)", goldenPath, i, reachFuzzN)
		indices = append(indices, i)
	}
	require.NotEmpty(t, indices, "golden %s lists no crash indices", goldenPath)
	return indices
}

// TestReachFuzz_RefusesEveryCycleTheResolverRecursesInto is a committed
// regression for a property that can only be checked against the raw,
// unrecoverable crash a stack overflow causes (see reachFuzzOracleEnv's own
// doc comment for why that needs a subprocess): every one of reachFuzzN
// documents this file's grammar generates from a fixed seed that crashes the
// vendored resolver must be refused as a cyclic reference.
//
// Running that subprocess oracle on all 200 documents on every test run does
// not fit inside a race-instrumented run's time budget — each spawn's own
// cost multiplies under -race, and the 200 of them exceed what a normal test
// timeout affords. So the crash labels are computed once, under -update,
// into testdata/reach_fuzz_crashes.golden, and every other run instead
// replays them in-process against the deterministic documents
// genReachFuzzDocs regenerates from the same seed: no subprocess, and fast
// even under -race.
//
// A dependency bump to speakeasy-api/openapi that changes the resolver's own
// behavior can make the golden stale — some index that used to be safe might
// start crashing, or stop. Regenerate it with -update when that dependency
// moves. If regenerating turns up a document that now crashes the resolver
// and this package does not refuse, that is a real finding to fix, not a
// golden to accept silently.
func TestReachFuzz_RefusesEveryCycleTheResolverRecursesInto(t *testing.T) {
	t.Parallel()
	docs := genReachFuzzDocs()

	if *update {
		regenerateReachFuzzGolden(t, docs, reachFuzzGoldenPath)
		return
	}

	indices := readReachFuzzGolden(t, reachFuzzGoldenPath)
	for _, i := range indices {
		spec := docs[i]
		assert.True(t, loadRefusedAsCycle(t, spec),
			"document %d crashes the raw resolver per %s but was not refused:\n%s",
			i, reachFuzzGoldenPath, spec)
	}
	t.Logf("%d/%d generated documents are known to crash the raw resolver; all refused",
		len(indices), len(docs))
}

// TestRegenerateAndReadReachFuzzGolden_RoundTrip drives regenerateReachFuzzGolden
// and readReachFuzzGolden directly — bypassing the -update flag entirely —
// against a two-document set (one known crash fixture, one that cannot
// possibly cycle) and a scratch golden path, rather than the real
// reachFuzzN-document sweep. It runs on every ordinary test invocation
// (unlike the real sweep, which -update alone gates) so both functions'
// logic, including the write/parse round trip and the header they share, are
// exercised without spawning 200 subprocesses.
func TestRegenerateAndReadReachFuzzGolden_RoundTrip(t *testing.T) {
	t.Parallel()
	docs := []string{anchorSelfTop, minimal31}
	goldenPath := filepath.Join(t.TempDir(), "roundtrip.golden")

	regenerateReachFuzzGolden(t, docs, goldenPath)

	raw, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(raw), "# Generated by:"), "the golden carries its own header")

	indices := readReachFuzzGolden(t, goldenPath)
	assert.Equal(t, []int{0}, indices, "only anchorSelfTop (index 0) crashes; minimal31 cannot cycle")
}
