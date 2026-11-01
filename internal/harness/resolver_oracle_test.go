// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness_test

// The differential oracle for the compiler's reading of references.
//
// The compiler binds schema $refs itself (compilers/openapi/internal/bind),
// never through speakeasy's resolver, which overflows the stack on a cycle
// (docs/parser-library.md#schema-cycle-overflow), and leaves to the resolver
// the reference-object chains it reports itself
// (docs/parser-library.md#reference-object-cycle). Whether either holds is not
// something a test of the compiler alone can show: a hang or a crash the
// resolver would meet is invisible until the compiler meets it too.
//
// So this asks the resolver as well. For each generated shape it compares what
// the compiler does against what the resolver does, both measured in a
// subprocess, because a hang and a stack overflow cannot be observed from
// inside the process they happen to, and because the compiler is subject to
// both if a schema $ref ever reaches the resolver again. An oracle that hangs
// instead of failing on the regression it exists for would be no oracle at all.
//
// It lives entirely in a _test.go file: the coverage gate counts statements in
// non-test files and the architecture test reads only non-test imports, so
// neither is affected by a harness that talks to the dependency directly.

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/exectest"
	"github.com/dexpace/morphic/ir"
)

// update rewrites outcomesGolden from what the resolver does now instead of
// comparing against it, the flag name and meaning ir/irtest gives its goldens.
var update = flag.Bool("update", false, "rewrite "+outcomesGolden+" instead of comparing against it")

// outcomesGolden pins what the resolver does with every generated shape.
const outcomesGolden = "testdata/resolver_outcomes.golden"

// probeEnv carries the spec to the helper process. Its presence is also what
// tells the helper it is the helper.
const probeEnv = "MORPHIC_RESOLVER_PROBE"

// probeTimeout is how long a shape gets before it counts as hanging. A shape
// that resolves at all resolves in milliseconds, so this only ever bounds a
// hang, and only a shape that hangs pays it; none does at the pinned version
// (docs/parser-library.md#reference-object-cycle). Subtests run in parallel,
// so it is the wall-clock cost of the slowest shape, not their sum.
const probeTimeout = 10 * time.Second

// probeMaxStack bounds the helper's goroutine stacks, as load's crash oracle
// bounds its own. At the runtime's default of 1 GB a stack overflow under -race
// can take longer than probeTimeout and read as a hang; these shapes are a few
// references deep, so nothing short of a runaway recursion comes near 64 MB.
const probeMaxStack = 64 << 20

// helperTimeout is the helper's own -test.timeout. It is twice probeTimeout, so
// the parent's kill always comes first and a hang is read as one, never as the
// panic the testing package's timeout would print.
const helperTimeout = 2 * probeTimeout

// The helper reports each half of its work on stdout as it finishes, so a shape
// that never returns is still attributable: losing both markers means the
// compiler did not survive, losing only the second means the resolver did not.
const (
	failedMarker  = "MORPHIC_COMPILER_FAILED="
	outcomeMarker = "MORPHIC_RESOLVER_OUTCOME="
)

// What the resolver did with a shape. The helper reports the first two itself;
// the parent reads the other three from how the helper stopped.
const (
	resolverClean      = "clean"      // returned, reporting nothing
	resolverErrors     = "errors"     // returned, reporting a problem with the spec
	resolverHung       = "hung"       // still running at probeTimeout
	resolverOverflowed = "overflowed" // the runtime's fatal stack overflow
	resolverPanicked   = "panicked"   // a panic nothing recovered
)

// unparsed is what the helper reports when the library could not parse the
// spec, so the resolver never ran and there is nothing to measure.
const unparsed = "unparsed"

// outcomes are the classes a golden line may hold.
var outcomes = []string{resolverClean, resolverErrors, resolverHung, resolverOverflowed, resolverPanicked}

const specHead = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"

// referencePosition is a document shape with one reference object in it, named
// by the pointer that reaches it. The template's %s takes the reference body.
type referencePosition struct {
	name    string
	pointer string
	tmpl    string
}

func referencePositions() []referencePosition {
	return []referencePosition{
		{
			name:    "path-item",
			pointer: "#/paths/~1a",
			tmpl:    specHead + "paths:\n  /a: %s\n",
		},
		{
			name:    "webhook",
			pointer: "#/webhooks/onA",
			tmpl:    specHead + "paths: {}\nwebhooks:\n  onA: %s\n",
		},
		{
			name:    "component-path-item",
			pointer: "#/components/pathItems/A",
			tmpl:    specHead + "paths:\n  /a: {$ref: '#/components/pathItems/A'}\ncomponents:\n  pathItems:\n    A: %s\n",
		},
		{
			name:    "component-response",
			pointer: "#/components/responses/A",
			tmpl: specHead + "paths:\n  /a:\n    get:\n      operationId: a\n      responses:\n" +
				"        \"200\": {$ref: '#/components/responses/A'}\ncomponents:\n  responses:\n    A: %s\n",
		},
		{
			name:    "schema",
			pointer: "#/components/schemas/A",
			tmpl:    specHead + "paths: {}\ncomponents:\n  schemas:\n    A: %s\n",
		},
	}
}

// pointerForm builds a reference body from the pointer that reaches it, so each
// form can be applied at every position.
type pointerForm struct {
	name string
	body func(pointer string) string
}

func pointerForms() []pointerForm {
	return []pointerForm{
		{"exact-self", func(p string) string { return fmt.Sprintf("{$ref: '%s'}", p) }},
		{"prefix-self-dangling", func(p string) string { return fmt.Sprintf("{$ref: '%s/t'}", p) }},
		{"prefix-self-trailing-space", func(p string) string { return fmt.Sprintf("{$ref: '%s '}", p) }},
		{"prefix-self-percent-encoded", func(p string) string {
			return fmt.Sprintf("{$ref: '%s'}", strings.Replace(p, "#/", "#/%", 1))
		}},
		{"prefix-self-resolving-sibling", func(p string) string {
			return fmt.Sprintf("{$ref: '%s/description', description: d}", p)
		}},
		{"prefix-self-sibling-back", func(p string) string {
			return fmt.Sprintf("{$ref: '%s/x', x: {$ref: '%s'}}", p, p)
		}},
		{"prefix-self-property-back", func(p string) string {
			return fmt.Sprintf("{$ref: '%s/properties/p', properties: {p: {$ref: '%s'}}}", p, p)
		}},
		{"prefix-self-empty-key-back", func(p string) string {
			return fmt.Sprintf("{$ref: '%s/properties/', properties: {'': {$ref: '%s'}}}", p, p)
		}},
		{"prefix-self-empty-token-stays", func(p string) string {
			return fmt.Sprintf("{$ref: '%s/x/', x: {$ref: '%s'}}", p, p)
		}},
		{"dangling-elsewhere", func(string) string { return "{$ref: '#/nope/nope'}" }},
		{"external-document", func(string) string { return "{$ref: 'other.yaml#/x'}" }},
	}
}

// referenceShape is one generated document, named position/form.
type referenceShape struct {
	name string
	spec string
}

// referenceShapes applies every form at every position, in that order, which is
// the order the golden lists them in.
func referenceShapes() []referenceShape {
	positions, forms := referencePositions(), pointerForms()
	shapes := make([]referenceShape, 0, len(positions)*len(forms))
	for _, pos := range positions {
		for _, form := range forms {
			shapes = append(shapes, referenceShape{
				name: pos.name + "/" + form.name,
				spec: fmt.Sprintf(pos.tmpl, form.body(pos.pointer)),
			})
		}
	}
	return shapes
}

// TestResolverOracle_VerdictMatchesResolverBehavior is the oracle. Across every
// generated shape it holds the compiler to three things:
//
//   - It survives every shape (probe).
//   - A shape the resolver cannot survive (a hang or a crash) fails it.
//   - A shape the resolver resolves cleanly does not.
//
// A shape the resolver survives but reports an error on binds neither way:
// where the compiler reads a pointer as RFC 6901 does and the resolver does
// not, the two may differ. Each shape's outcome is also pinned in
// outcomesGolden: a bump turning a crash into an error changes what the
// library does, and only the pin sees it (GitHub #574).
func TestResolverOracle_VerdictMatchesResolverBehavior(t *testing.T) {
	t.Parallel()
	if os.Getenv(probeEnv) != "" {
		t.Skip("helper process")
	}

	shapes := referenceShapes()
	pinned := pinnedOutcomes(t, shapes)
	measured := measuredOutcomes(t, shapes)
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			failed, outcome := probe(t, shape.spec)
			measured.record(shape.name, outcome)
			switch outcome {
			case resolverHung, resolverOverflowed, resolverPanicked:
				assert.True(t, failed,
					"the resolver cannot survive this, so the compiler must report what is wrong "+
						"with it\nresolver outcome: %s\nspec:\n%s", outcome, shape.spec)
			case resolverClean:
				assert.False(t, failed,
					"the resolver resolves this cleanly, so failing it is an over-refusal"+
						"\nspec:\n%s", shape.spec)
			}
			if !*update {
				assert.Equal(t, pinned[shape.name], outcome,
					"the resolver's outcome for this shape changed, so a guard the compiler keeps "+
						"against it may be obsolete or missing: decide which, then record it with "+
						"-update\nspec:\n%s", shape.spec)
			}
		})
	}
}

// pinnedOutcomes reads outcomesGolden, which must name every shape exactly once
// and nothing else. Under -update there is nothing to compare, so it reads none.
func pinnedOutcomes(t *testing.T, shapes []referenceShape) map[string]string {
	t.Helper()
	if *update {
		return nil
	}
	pinned := readOutcomes(t, outcomesGolden)
	names := make([]string, len(shapes))
	for i, s := range shapes {
		names[i] = s.name
	}
	require.ElementsMatch(t, names, slices.Collect(maps.Keys(pinned)),
		"%s must pin every generated shape and nothing else; regenerate it with -update", outcomesGolden)
	return pinned
}

// readOutcomes parses a golden of "shape outcome" lines, skipping blank lines
// and "#" comments. A malformed line, a duplicate shape or an outcome no probe
// reports fails rather than being read past.
func readOutcomes(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err, "read %s (run with -update to create it)", path)
	t.Cleanup(func() { require.NoError(t, f.Close()) })

	pinned := map[string]string{}
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		require.Len(t, fields, 2, "%s: %q is not a shape and an outcome", path, line)
		name, outcome := fields[0], fields[1]
		require.Contains(t, outcomes, outcome, "%s: %q is no outcome a probe reports", path, line)
		_, dup := pinned[name]
		require.False(t, dup, "%s pins %s twice", path, name)
		pinned[name] = outcome
	}
	require.NoError(t, lines.Err())
	return pinned
}

// outcomeRecord collects what the parallel subtests measured, so -update can
// write the golden once every one of them is done.
type outcomeRecord struct {
	mu   sync.Mutex
	seen map[string]string
}

func (r *outcomeRecord) record(name, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[name] = outcome
}

// measuredOutcomes returns the record the subtests fill. Under -update it also
// writes the golden from it, in a cleanup, which runs after the parallel
// subtests finish; a shape left unmeasured fails instead of being dropped.
func measuredOutcomes(t *testing.T, shapes []referenceShape) *outcomeRecord {
	t.Helper()
	r := &outcomeRecord{seen: map[string]string{}}
	if !*update {
		return r
	}
	t.Cleanup(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		require.Len(t, r.seen, len(shapes), "not every shape was measured, so %s was not written", outcomesGolden)
		writeOutcomes(t, outcomesGolden, shapes, r.seen)
	})
	return r
}

const outcomesHeader = `# Generated by:
#   go test ./internal/harness -run TestResolverOracle_VerdictMatchesResolverBehavior -update
#
# What the parser library's resolver does with each reference shape
# referenceShapes generates, measured in a subprocess with nothing in its way:
# clean, errors, hung, overflowed or panicked. A line that changes is a library
# change: one the compiler may need to answer, or one that retires something it
# keeps because of the library. Decide which before regenerating; the
# regeneration is not the decision.
`

// writeOutcomes writes the golden, one shape per line in generation order.
func writeOutcomes(t *testing.T, path string, shapes []referenceShape, seen map[string]string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(outcomesHeader)
	for _, s := range shapes {
		fmt.Fprintf(&sb, "%s %s\n", s.name, seen[s.name])
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(sb.String()), 0o644))
}

// probe runs one shape in a subprocess and reports the compiler's verdict,
// whether it reported an error, and what the resolver did. A helper that stops
// in a way none of the outcomes describes, or a shape the library cannot
// parse, fails the shape rather than being filed under the nearest outcome.
func probe(t *testing.T, spec string) (failed bool, outcome string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := exectest.Command(ctx, t, os.Args[0],
		"-test.run=^TestResolverOracle_ProbeHelper$", "-test.timeout="+helperTimeout.String())
	cmd.Env = append(cmd.Env, probeEnv+"="+spec)

	out, err := cmd.CombinedOutput()
	text := string(out)

	verdict, sawCompiler := markerValue(text, failedMarker)
	if !sawCompiler {
		t.Fatalf("the compiler did not survive this shape (%s)\nspec:\n%s\n%s",
			stopReason(ctx, err), spec, truncate(out))
	}
	failed = verdict == "true"

	returned, sawResolver := markerValue(text, outcomeMarker)
	switch {
	case sawResolver && returned == unparsed:
		t.Fatalf("the library could not parse this shape, so the resolver never ran\nspec:\n%s", spec)
	case sawResolver && (returned == resolverClean || returned == resolverErrors):
		return failed, returned
	case sawResolver:
		t.Fatalf("the helper reported an outcome the oracle does not know: %q", returned)
	case ctx.Err() != nil:
		return failed, resolverHung
	case strings.Contains(text, "fatal error: stack overflow"):
		return failed, resolverOverflowed
	case strings.Contains(text, "panic: "):
		return failed, resolverPanicked
	}
	t.Fatalf("the resolver process stopped in a way the oracle does not recognize (%v)\nspec:\n%s\n%s",
		err, spec, truncate(out))
	return false, ""
}

// TestResolverOracle_ProbeHelper is the subprocess body. It compiles the spec in
// probeEnv, reports the verdict, then resolves the same spec with nothing in the
// way. It asserts nothing: hanging or dying is the signal, and the parent reads
// it from which markers arrived.
func TestResolverOracle_ProbeHelper(t *testing.T) {
	spec := os.Getenv(probeEnv)
	if spec == "" {
		t.Skip("not the helper process")
	}
	ctx := context.Background()
	debug.SetMaxStack(probeMaxStack)

	// The compiler first, its verdict printed before anything else runs: if this
	// call is what hangs, the missing marker is the finding.
	_, diags, err := openapi.New().Compile(ctx,
		[]compilers.Source{{Path: "probe.yaml", Data: []byte(spec)}}, compilers.Options{})
	fmt.Printf("%s%v\n", failedMarker, err == nil && ir.HasError(diags))

	// Then the resolver with nothing in its way, which is the measurement the
	// compiler's verdict is held to.
	doc, _, err := soa.Unmarshal(ctx, strings.NewReader(spec))
	if err != nil || doc == nil {
		fmt.Printf("%s%s\n", outcomeMarker, unparsed)
		return
	}
	resolveErrs, err := doc.ResolveAllReferences(ctx, soa.ResolveAllOptions{
		OpenAPILocation:     "probe.yaml",
		DisableExternalRefs: true,
	})
	outcome := resolverClean
	if err != nil || len(resolveErrs) > 0 {
		outcome = resolverErrors
	}
	fmt.Printf("%s%s\n", outcomeMarker, outcome)
}

// markerValue reads the value the helper printed for a marker.
func markerValue(text, marker string) (string, bool) {
	_, rest, ok := strings.Cut(text, marker)
	if !ok {
		return "", false
	}
	if j := strings.IndexByte(rest, '\n'); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest), true
}

// stopReason names why a child stopped, for the failure message.
func stopReason(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "hung"
	}
	return fmt.Sprintf("died: %v", err)
}

// truncate bounds a failing subprocess's output so a stack dump does not bury
// the assertion that reported it.
func truncate(out []byte) string {
	const limit = 400
	if len(out) <= limit {
		return string(out)
	}
	return string(out[:limit]) + "\n... (truncated)"
}
