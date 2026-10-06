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
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/internal/leakcheck"
	"github.com/dexpace/morphic/ir"
)

// update rewrites testdata/reach_fuzz_crashes.golden instead of comparing
// against it, the same flag name and meaning ir/irtest's golden helper uses
// for IR documents (this package cannot import ir/irtest itself: it sits
// below ir/irtest in the layering, and the golden here is a list of indices,
// not an ir.Document).
var update = flag.Bool("update", false, "rewrite testdata/reach_fuzz_crashes.golden instead of comparing")

// reachFuzzOracleEnv, when set, makes this test binary act as the crash
// oracle: TestMain reads the spec it names, runs the load pipeline over it with
// the reference-chain check off and a bounded stack, and exits. A stack
// overflow cannot be recovered in-process, so it has to happen in a process the
// test can afford to lose.
const reachFuzzOracleEnv = "MORPHIC_REACH_FUZZ_ORACLE_SPEC"

// TestMain lets this package act as its own crash-oracle subprocess. Every
// other test in the package is unaffected: the environment variable is unset
// unless reachFuzzSubprocessCrashed sets it for a child process it spawns. They
// run through leakcheck.Main, for the tests that start goroutines.
func TestMain(m *testing.M) {
	if specPath := os.Getenv(reachFuzzOracleEnv); specPath != "" {
		os.Exit(runReachFuzzOracle(specPath))
	}
	leakcheck.Main(m)
}

// runReachFuzzOracle runs this package's own load pipeline over the spec at
// specPath with the reference-chain check turned off, and returns the process
// exit code: 0 if Load returns at all, nonzero for an I/O failure. A genuine
// stack overflow never returns: the Go runtime ends the process with status 2,
// which reachFuzzSubprocessCrashed reads.
//
// The pipeline, not the raw library, is the oracle because it is what the
// compiler hands the resolver, and load holds "#/$defs/..." references out of
// the resolver's pass (GitHub #557): the library's own lookup never runs.
func runReachFuzzOracle(specPath string) int {
	debug.SetMaxStack(64 << 20)
	data, err := os.ReadFile(specPath)
	if err != nil {
		return 3
	}
	_, _, _ = Load(context.Background(), 0, compilers.Source{Path: specPath, Data: data}, Options{chainCheck: noChainCheck})
	return 0
}

// noChainCheck is chainCheck turned off: the oracle's pipeline runs every
// other refusal Load has, so what crashes is exactly what the check stands
// between the resolver and.
func noChainCheck(context.Context, scan.Locator, *yaml.Node, *soa.OpenAPI) (ir.Diagnostic, bool) {
	return ir.Diagnostic{}, false
}

// reachFuzzSubprocessCrashed re-execs this test binary as the oracle over
// specPath and reports whether the pipeline overflowed its stack. Exit status 2
// is also what a panic ends with, so the fatal error's own text is required: a
// panic is a different finding and fails the test rather than counting as one.
func reachFuzzSubprocessCrashed(ctx context.Context, t *testing.T, specPath string) bool {
	t.Helper()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), reachFuzzOracleEnv+"="+specPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if !assert.ErrorAs(t, err, &exitErr, "the oracle subprocess must exit rather than fail to start") ||
		exitErr.ExitCode() != 2 {
		return false
	}
	return assert.Contains(t, string(out), "fatal error: stack overflow",
		"the oracle exited with status 2 for a reason other than the stack overflow it looks for")
}

// reachFuzzRefs, reachFuzzIDs and reachFuzzAnchors are the fixed vocabularies
// the grammar below draws $ref/$id/$anchor values from, the shapes GitHub #526
// and #546 report, so they run as a committed regression: random mixes of "#",
// "#/" and $defs-relative pointers, cross-component references, and
// relative/absolute $id values.
var (
	reachFuzzRefs = []string{
		"#a", "#b", "#/$defs/m", "#/$defs/n", "#/$defs/m/properties/p",
		"#/components/schemas/A", "#/components/schemas/B", "#/components/schemas/C",
		"#/components/schemas/A/$defs/m", "#/components/schemas/B/$defs/n",
		"#/components/schemas/A/properties/p", "#/components/schemas/B/properties/q",
		"https://x.test/a", "https://x.test/a#a", "https://x.test/b#/$defs/m", "a.json", "#/x-s", "#/x-s/$defs/m",
		// Spellings the resolver reads differently from a byte-for-byte reader:
		// a percent-encoded pointer, a second '#', and URIs a '.', a '..' or a
		// percent-encoding turns into another path.
		"#/%24defs/m", "#/$defs%2Fn", "#/$defs/m#x", "#a#x", "#", "#/", "./a.json", "foo/..", ".",
		"https://x.test/a/", "https://x.test/a%20b", "https://x.test/a b",
	}
	reachFuzzIDs = []string{
		"https://x.test/a", "https://x.test/b", "a.json", "https://x.test/a/", "https://x.test/a%20b", "./a.json", "a/..",
	}
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
	// yamlAnchor names a YAML anchor the rendered mapping carries; merge names
	// the anchor of an earlier mapping it merges in with a `<<` key.
	yamlAnchor, merge string
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
	if s.yamlAnchor != "" {
		sb.WriteString("&" + s.yamlAnchor + " ")
	}
	sb.WriteByte('{')
	first := true
	if s.merge != "" {
		sb.WriteString("<<: *" + s.merge)
		first = false
	}
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
// {A, B, C}, each an independently generated reachFuzzSchema. Sometimes the
// first is a YAML anchor that a later one merges.
func genFuzzDoc(rng *rand.Rand) string {
	var sb bytes.Buffer
	sb.WriteString("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n")
	if rng.Float64() < 0.2 {
		sb.WriteString("x-s: ")
		genFuzzSchema(rng, 1).writeJSON(&sb)
		sb.WriteByte('\n')
	}
	sb.WriteString("components:\n  schemas:\n")
	keys := fuzzSample(rng, []string{"A", "B", "C"})
	schemas := make([]*reachFuzzSchema, len(keys))
	for i := range keys {
		schemas[i] = genFuzzSchema(rng, 2)
	}
	if len(keys) > 1 && rng.Float64() < 0.35 {
		schemas[0].yamlAnchor = "base"
		schemas[1+rng.Intn(len(keys)-1)].merge = "base"
	}
	for i, k := range keys {
		sb.WriteString("    " + k + ": ")
		schemas[i].writeJSON(&sb)
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
# generates, in order, from rand.New(rand.NewSource(reachFuzzSeed)), crash this
# package's own load pipeline with an unrecoverable stack overflow when its
# reference-chain check is turned off (runReachFuzzOracle). math/rand's v1
# source (used here, not math/rand/v2) is documented to produce the same
# sequence for a given seed across Go versions, so genReachFuzzDocs reproduces
# these exact documents on every run without this file needing to store them
# itself.
#
# A dependency bump to speakeasy-api/openapi that changes the resolver's own
# behavior, or a change to how load drives it, can make this file stale.
# Regenerate it with -update. If that finds a document which now crashes the
# pipeline but TestReachFuzz_RefusesEveryCycleTheResolverRecursesInto does not
# refuse, that is a real finding to fix, not a golden to accept as given.
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

// regenerateReachFuzzGolden runs the subprocess oracle over docs and rewrites
// goldenPath with the indices that overflow the stack. Only -update runs it over
// the real sweep, because a subprocess per document does not fit a
// race-instrumented run. TestRegenerateAndReadReachFuzzGolden_RoundTrip calls
// it on two documents, so every line is covered on ordinary runs.
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

// TestReachFuzz_RefusesEveryCycleTheResolverRecursesInto requires that every
// generated document the golden records as overflowing the pipeline's stack is
// refused. The labels come from the subprocess oracle under -update, since
// running it for all reachFuzzN documents does not fit a race run; every other
// run replays them in-process. A dependency bump can make the golden stale:
// regenerate it, and treat a document it finds crashing but unrefused as a
// finding, not a golden to accept.
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
			"document %d crashes the pipeline per %s but was not refused:\n%s",
			i, reachFuzzGoldenPath, spec)
	}
	t.Logf("%d/%d generated documents are known to crash the pipeline without the check; all refused",
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
