package operation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/operation"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// This half of the suite drives whole documents through the compiler. The
// operation walk is what turns a path item into operations, parameters and
// content, and the positions it hoists only exist once it has run — so the
// fixtures are specs, not hand-built values, and reaching the compiler needs an
// external test package.

// sourceOf wraps a spec string as a compilers.Source.
func sourceOf(src string) compilers.Source {
	return compilers.Source{Path: "spec.yaml", Data: []byte(src)}
}

// parseFull runs the whole public compiler pipeline over src.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{sourceOf(src)},
		compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// lowerServiceSpec compiles src and returns the document, its single service,
// and every diagnostic.
func lowerServiceSpec(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags := parseFull(t, src)
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}

// findOp returns the operation whose source name matches.
func findOp(t *testing.T, doc *ir.Document, source string) ir.Operation {
	t.Helper()
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if op.Name.Source == source {
				return op
			}
		}
	}
	t.Fatalf("operation %q not found", source)
	return ir.Operation{}
}

// firstOp returns the operation at svc.Groups[0].Operations[0], requiring both
// to be non-empty first rather than letting a malformed fixture fail with a
// bare index-out-of-range panic.
func firstOp(t *testing.T, svc ir.Service) ir.Operation {
	t.Helper()
	require.NotEmpty(t, svc.Groups, "service has no operation groups")
	require.NotEmpty(t, svc.Groups[0].Operations, "first group has no operations")
	return svc.Groups[0].Operations[0]
}

// requireNoErrorDiags fails the test if any diagnostic has error severity.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}

// pathsSpec wraps a paths block in a minimal 3.1 document with no components.
func pathsSpec(paths string) string {
	return pathsSpecVer("3.1.0", paths)
}

// pathsSpecVer wraps a paths block in a minimal document of the given OpenAPI
// version, with no components.
func pathsSpecVer(version, paths string) string {
	return "openapi: " + version + "\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths:\n" + paths
}

// typeByName returns the named component schema's lowered TypeDef.
func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[ir.TypeID("t/openapi/components/schemas/"+name)]
}

// strNode builds a bare string-scalar yaml.Node.
func strNode(val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val}
}

// assertHasCode requires diags to carry a diagnostic with the given code at the
// given severity.
func assertHasCode(t *testing.T, diags []ir.Diagnostic, code string, sev ir.Severity) {
	t.Helper()
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			return
		}
	}
	t.Fatalf("expected a %v diagnostic with code %q, got %+v", sev, code, diags)
}

// hasDiag reports whether diags contains a diagnostic with the exact code, at
// any severity.
func hasDiag(diags []ir.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// hasDiagCodeAt reports whether diags carries code at exactly pointer.
func hasDiagCodeAt(diags []ir.Diagnostic, code, pointer string) bool {
	for _, d := range diags {
		if d.Code == code && d.Provenance.Pointer == pointer {
			return true
		}
	}
	return false
}

// countDiagsAt counts the diagnostics matching code and sev exactly. code is an
// exact match with no wildcard: countDiagsAt(diags, "", ir.SeverityError)
// matches only diagnostics whose code is literally empty.
func countDiagsAt(diags []ir.Diagnostic, code string, sev ir.Severity) int {
	var n int
	for _, d := range diags {
		if d.Code == code && d.Severity == sev {
			n++
		}
	}
	return n
}

// firstDegradedWarning returns the first degraded-construct warning in diags,
// and whether one was found.
func firstDegradedWarning(diags []ir.Diagnostic) (ir.Diagnostic, bool) {
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Severity == ir.SeverityWarning {
			return d, true
		}
	}
	return ir.Diagnostic{}, false
}

// diagMessageAt returns the message of the single diagnostic matching code,
// severity and provenance pointer.
func diagMessageAt(t *testing.T, diags []ir.Diagnostic, code string, sev ir.Severity, pointer string) string {
	t.Helper()
	var found []string
	for _, d := range diags {
		if d.Code == code && d.Severity == sev && d.Provenance.Pointer == pointer {
			found = append(found, d.Message)
		}
	}
	require.Len(t, found, 1, "want exactly one %v %q at %q, got %+v", sev, code, pointer, diags)
	return found[0]
}

// indexBy builds a lookup keyed by key(item).
func indexBy[T any, K comparable](items []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(items))
	for _, item := range items {
		out[key(item)] = item
	}
	return out
}

// propsByWire indexes a model's properties by wire name.
func propsByWire(props []ir.Property) map[string]ir.Property {
	return indexBy(props, func(p ir.Property) string { return p.WireName })
}

// serviceWithGrouping runs the phases beneath the service walk and then the walk
// itself under a chosen grouping strategy. It names the strategy directly rather
// than routing one through the compiler's public options, because grouping is
// the only thing these tests vary and the projection in between is the
// compiler's business, not theirs.
func serviceWithGrouping(t *testing.T, src string, grouping lowering.GroupingStrategy) (ir.Service, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, loadDiags, err := load.Load(t.Context(), 0, sourceOf(src), load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)

	types := compile.NewTypes(0)
	c := lowering.New(0, loadedDoc.Doc, loadedDoc.Source, grouping, lowering.StreamingMedia{}, overlay.Origin{})
	var anchors schema.AnchorIndex
	var acc compile.Diags
	acc.AppendAll(schema.LowerComponentSchemas(c, types, &anchors))

	svc, _, svcDiags := operation.LowerService(c, types, &anchors, make(map[string]string))
	acc.AppendAll(svcDiags)
	return svc, append(loadDiags, acc.List()...)
}

// The inline-probe helpers below also exist in the schema package's tests and in
// the compiler's own. Each package needs them and none can see another's test
// scaffolding; each copy is held to its meaning by the tests that use it.
// The inline-probe helpers below are duplicated in the schema package's own
// tests. Both sides need them and neither package can see the other's test
// scaffolding, which is the cost of the split; each copy is held to its meaning
// by the tests that use it, so a copy that drifts fails on its own side.
// inlineProbeBody is the body every inline-position case below writes: one
// annotation of each kind attachDeclaredAnnotations reads, one validation-only
// keyword, and one value constraint — all of them position-scoped, so a
// position that lowers this to the shared string primitive loses every one. All
// three documentation keywords are here because a home that keeps only the
// description passes a probe that writes only a description.
const inlineProbeBody = `{type: string, title: SUM, description: DOC, ` +
	`externalDocs: {url: 'https://e.example', description: ED}, deprecated: true, ` +
	`example: abc, x-vendor: V, xml: {name: X}, not: {const: N}, maxLength: 3}`

// assertProbeDocsKept checks all three documentation keywords inlineProbeBody
// writes reached d, wherever the position's home turned out to be.
func assertProbeDocsKept(t *testing.T, d ir.Docs) {
	t.Helper()
	assert.Equal(t, "SUM", d.Summary, "title")
	assert.Equal(t, "DOC", d.Description, "description")
	if assert.Len(t, d.ExternalDocs, 1, "externalDocs") {
		assert.Equal(t, "https://e.example", d.ExternalDocs[0].URL)
		assert.Equal(t, "ED", d.ExternalDocs[0].Description)
	}
}

// assertProbeExample checks the single example inlineProbeBody writes reached
// the home under test with its value intact.
func assertProbeExample(t *testing.T, examples []ir.Example) {
	t.Helper()
	if !assert.Len(t, examples, 1, "examples") {
		return
	}
	require.NotNil(t, examples[0].Value)
	assert.Equal(t, "abc", examples[0].Value.Str)
}

// assertInfoDiagAt requires one info diagnostic stamped at pointer.
func assertInfoDiagAt(t *testing.T, diags []ir.Diagnostic, pointer string) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == ir.SeverityInfo && d.Provenance.Pointer == pointer {
			return
		}
	}
	assert.Fail(t, "nothing announced this", "no info diagnostic at %q; got %+v", pointer, diags)
}
