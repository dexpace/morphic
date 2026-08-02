package harness_test

// The differential oracle for the compiler's reference refusals.
//
// compilers/openapi/internal/scan refuses documents that would hang or crash
// speakeasy's resolver. That refusal is a *model* of one version's behavior, and
// a model can be wrong in two directions: too narrow lets a hang through, too
// wide refuses a document that compiles. Neither shows up in a test that only
// asserts what the scan says, because the scan is the thing under test.
//
// So this asks the resolver instead. For each generated shape it compares what
// the compiler does against what the resolver actually does, both measured in a
// subprocess — because a deadlock and a stack overflow cannot be observed from
// inside the process they happen to, and because the compiler is subject to both
// if its refusal ever regresses. An oracle that hangs instead of failing on the
// regression it exists for would be no oracle at all.
//
// It lives entirely in a _test.go file: the coverage gate counts statements in
// non-test files and the architecture test reads only non-test imports, so
// neither is affected by a harness that talks to the dependency directly.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// probeEnv carries the spec to the helper process. Its presence is also what
// tells the helper it is the helper.
const probeEnv = "MORPHIC_RESOLVER_PROBE"

// probeTimeout is how long a shape gets before it counts as hanging. A shape
// that resolves at all resolves in milliseconds, so this only ever bounds a
// deadlock, and only the shapes that deadlock pay it. Subtests run in parallel,
// so it is the wall-clock cost of the slowest shape, not their sum.
const probeTimeout = 10 * time.Second

// The helper reports each half of its work on stdout as it finishes, so a shape
// that never returns is still attributable: losing both markers means the
// compiler did not survive, losing only the second means the resolver did not.
const (
	refusedMarker = "MORPHIC_COMPILER_REFUSED="
	errorMarker   = "MORPHIC_RESOLVE_ERRORS="
)

// What the resolver did with a shape.
const (
	resolverClean  = "resolved-clean"  // returned, reporting nothing
	resolverErrors = "resolved-errors" // returned, reporting a problem with the spec
	resolverHung   = "hung"            // still running at probeTimeout
	resolverDied   = "died"            // exited non-zero: a runtime fatal error
)

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
		{"dangling-elsewhere", func(string) string { return "{$ref: '#/nope/nope'}" }},
		{"external-document", func(string) string { return "{$ref: 'other.yaml#/x'}" }},
	}
}

// TestResolverOracle_RefusalMatchesResolverBehavior is the oracle. Across every
// generated shape it holds the compiler to two things, in the two directions a
// model of someone else's behavior can be wrong:
//
//   - A shape the resolver cannot survive must be refused. Letting one through
//     is the bug this guard exists for: a hang, or a process taken down.
//   - A shape the resolver resolves cleanly must not be refused. Refusing one is
//     an over-refusal — a document that compiled yesterday and does not today.
//
// A shape the resolver survives but reports an error on binds neither way. The
// document fails whichever path it takes, so refusing it early with a clearer
// message and leaving the resolver to name it are both defensible, and that
// choice belongs to the scan rather than to this oracle. The distinction is why
// the helper reports what the resolver *said* rather than only that it returned:
// an exact self-reference at a document position resolves with a
// circular-reference error, and reading that as "fine" would have this oracle
// demand an over-refusal be introduced.
func TestResolverOracle_RefusalMatchesResolverBehavior(t *testing.T) {
	t.Parallel()
	if os.Getenv(probeEnv) != "" {
		t.Skip("helper process")
	}

	for _, pos := range referencePositions() {
		for _, form := range pointerForms() {
			name, spec := pos.name+"/"+form.name, fmt.Sprintf(pos.tmpl, form.body(pos.pointer))
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				refused, outcome := probe(t, spec)
				switch outcome {
				case resolverHung, resolverDied:
					assert.True(t, refused,
						"the resolver cannot survive this, so the compiler must refuse it before "+
							"reaching it\nresolver outcome: %s\nspec:\n%s", outcome, spec)
				case resolverClean:
					assert.False(t, refused,
						"the resolver resolves this cleanly, so refusing it is an over-refusal"+
							"\nspec:\n%s", spec)
				default:
					t.Logf("resolver reported an error; refusing is the scan's call (refused=%v)", refused)
				}
			})
		}
	}
}

// probe runs one shape in a subprocess and reports the compiler's verdict and
// what the resolver did.
func probe(t *testing.T, spec string) (refused bool, outcome string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0],
		"-test.run=^TestResolverOracle_ProbeHelper$", "-test.timeout="+probeTimeout.String())
	cmd.Env = append(os.Environ(), probeEnv+"="+spec)

	out, err := cmd.CombinedOutput()
	text := string(out)

	verdict, sawCompiler := markerValue(text, refusedMarker)
	if !sawCompiler {
		t.Fatalf("the compiler did not survive this shape (%s), so its refusal has regressed"+
			"\nspec:\n%s\n%s", stopReason(ctx, err), spec, truncate(out))
	}
	refused = verdict == "true"

	errCount, sawResolver := markerValue(text, errorMarker)
	switch {
	case !sawResolver && ctx.Err() != nil:
		return refused, resolverHung
	case !sawResolver:
		t.Logf("resolver process exited non-zero (%v):\n%s", err, truncate(out))
		return refused, resolverDied
	case errCount == "0":
		return refused, resolverClean
	default:
		return refused, resolverErrors
	}
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

	// The compiler first, its verdict printed before anything else runs: if this
	// call is what hangs, the missing marker is the finding.
	_, diags, err := openapi.New().Compile(ctx,
		[]compilers.Source{{Path: "probe.yaml", Data: []byte(spec)}}, compilers.Options{})
	fmt.Printf("%s%v\n", refusedMarker, err == nil && hasCyclicRef(diags))

	// Then the resolver with nothing in its way, which is the measurement the
	// compiler's refusal is a model of.
	doc, _, err := soa.Unmarshal(ctx, strings.NewReader(spec))
	if err != nil || doc == nil {
		fmt.Printf("%s1\n", errorMarker) // never reached the resolver: a parse problem
		return
	}
	resolveErrs, err := doc.ResolveAllReferences(ctx, soa.ResolveAllOptions{
		OpenAPILocation:     "probe.yaml",
		DisableExternalRefs: true,
	})
	count := len(resolveErrs)
	if err != nil {
		count++
	}
	fmt.Printf("%s%d\n", errorMarker, count)
}

// hasCyclicRef reports whether the compiler refused a spec as a degenerate
// reference. Other error diagnostics — an unresolved ref, a validation failure —
// are not refusals of this kind and do not count.
func hasCyclicRef(diags []ir.Diagnostic) bool {
	for _, d := range diags {
		if d.Code == "openapi/cyclic-ref" && d.Severity == ir.SeverityError {
			return true
		}
	}
	return false
}

// markerValue reads the value the helper printed for a marker.
func markerValue(text, marker string) (string, bool) {
	i := strings.Index(text, marker)
	if i < 0 {
		return "", false
	}
	rest := text[i+len(marker):]
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
	const max = 400
	if len(out) <= max {
		return string(out)
	}
	return string(out[:max]) + "\n... (truncated)"
}
