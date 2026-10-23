package schema_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// This package's tests come in two halves, and the split is not stylistic.
// Everything reachable through the exported surface lives out here, where the
// whole compiler is importable and a test can drive a spec end to end; the tests
// in package schema are the ones that read an unexported lowering directly, and
// nothing importable from here would let them. Coverage lands on the schema
// package either way: the package under test is the instrumented one, whichever
// side of the boundary the caller sits on.

// parseFull runs the whole public compiler pipeline over src. It reaches back
// through openapi deliberately: a schema position under a parameter or a request
// body is only hoisted once the operation walk has resolved its way down to it,
// and no amount of driving this package alone produces that. That reach is also
// why openapitest cannot hold it — see that package's doc comment.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// lowerSpecOnly is schema.LowerSpec without the load phase's diagnostics
// mixed in. A $ref the lowering cannot follow is reported both by the load
// phase, which binds it, and by the lowering itself, at the same pointer
// (compilers/openapi's withoutRereported drops the second once both reach
// the compiler). A test that wants to pin what the lowering itself reports —
// not what a full compile does with the two reports after deduplication —
// needs the lowering's diagnostics on their own.
func lowerSpecOnly(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	l, _ := schema.LoweredFor(t, src)
	l.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types))
	doc := l.Document(t)
	return doc, l.Diags.List()
}

// componentID is the stable TypeID of a components-named schema, or of a
// sub-schema beneath one ("Holder/properties/inner"). It stays a per-package
// copy for the reason openapitest's doc comment gives: a spelled-out ID belongs
// in a test file, which the idderivations rule does not hold.
func componentID(name string) ir.TypeID {
	return ir.TypeID("t/openapi/components/schemas/" + name)
}

// typeByName returns the named component schema's lowered TypeDef.
func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[componentID(name)]
}

// conflictDiags returns every conflicting-redeclaration diagnostic in diags.
func conflictDiags(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.ConflictingRedecl {
			out = append(out, d)
		}
	}
	return out
}

// settledSpec is nonBareSpec with the components after the schema composing
// them, so a redeclaration naming one is judged once the registry is whole.
func settledSpec(t *testing.T, extra, schemaA, schemaB string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return schema.LowerSpec(t, openapitest.ComponentSpec(
		"    T:\n      allOf:\n"+
			"        - type: object\n          properties: {v: "+schemaA+"}\n"+
			"        - type: object\n          properties: {v: "+schemaB+"}\n"+extra))
}

// reported renders diags as a sorted multiset of what was reported where, since
// diagnostics are appended in the order the walk reaches them.
func reported(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s: %s", d.Severity, d.Code, d.Provenance.Pointer, d.Message))
	}
	slices.Sort(out)
	return out
}

// compileBeside compiles root as the root.yaml of a directory holding files,
// with external references allowed, and returns the document, its diagnostics
// and the directory. The one document the source reads is Sources[1].
func compileBeside(t *testing.T, files map[string]string, root string) (*ir.Document, []ir.Diagnostic, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)}},
		compilers.Options{Reader: reader.Disk{}, FormatOptions: openapi.Options{AllowExternalRefs: true}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, doc.Sources, 2, "the source and the one document it reads")
	return doc, diags, dir
}

// onlyBodyTarget returns the type of the one response body of the document's
// one operation.
func onlyBodyTarget(t *testing.T, doc *ir.Document) ir.TypeID {
	t.Helper()
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	require.Len(t, doc.Services[0].Groups[0].Operations, 1)
	op := doc.Services[0].Groups[0].Operations[0]
	require.Len(t, op.Responses, 1)
	return openapitest.BodyTarget(t, op.Responses[0].Payload)
}

// withCode returns the diagnostics among diags that carry code.
func withCode(diags []ir.Diagnostic, code string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

// assertProbeAnnotationsKept checks every keyword openapitest.InlineProbeBody writes
// survived onto the node the declaration owns.
func assertProbeAnnotationsKept(t *testing.T, sc *ir.Scalar) {
	t.Helper()
	openapitest.AssertProbeDocsKept(t, sc.Docs)
	assert.NotNil(t, sc.Deprecation, "deprecation")
	openapitest.AssertProbeExample(t, sc.Examples)
	if assert.NotNil(t, sc.XML, "xml hints") {
		assert.Equal(t, "X", sc.XML.Name)
	}
	vendor, ok := sc.Unmodeled["openapi:x-vendor"]
	if assert.True(t, ok, "vendor extension: got %v", sc.Unmodeled) {
		assert.JSONEq(t, `"V"`, string(vendor.Value))
		assert.Equal(t, ir.ReasonVendorExtension, vendor.Reason)
	}
	not, ok := sc.Unmodeled["openapi:not"]
	if assert.True(t, ok, "validation-only keyword: got %v", sc.Unmodeled) {
		assert.Equal(t, ir.ReasonValidationOnly, not.Reason)
	}
	if assert.NotNil(t, sc.Constraints, "value constraints") {
		require.NotNil(t, sc.Constraints.MaxLength)
		assert.Equal(t, int64(3), *sc.Constraints.MaxLength)
	}
}

// stolenPosition is an inline position an outside $ref can name by pointer:
// the component block declaring it, and the node its declaration owns. The
// outside component is spelled around the owner so both declaration orders of
// the same document are available.
type stolenPosition struct {
	name  string
	owner string
	id    ir.TypeID
}

// spec writes the position's owner and an outside $ref naming it, the reference
// declared first or last. Components lower in source order, so the two are the
// two orders in which the pointer's node can be reached.
func (p stolenPosition) spec(refFirst bool) string {
	outsider := "    Outsider: {$ref: '#" + strings.TrimPrefix(string(p.id), "t/anon") + "'}\n"
	if refFirst {
		return openapitest.ComponentSpec(outsider + p.owner)
	}
	return openapitest.ComponentSpec(p.owner + outsider)
}

// orderInvariantIR compares two whole IR documents, minus what differs by
// construction when the same components are declared in two orders:
// SourceInfo.Hash digests the source bytes, which are what is permuted, and a
// Provenance.Position is a line and column of those bytes.
//
// Naming.Hint is deliberately not excluded. The namers that can reach a pointer
// (the enclosing declaration and an outside $ref's pointer walk) agree at every
// position either can reach (GitHub #181, #281, #353), so the tests permuting
// those positions catch a regression only while nothing here hides the
// difference.
func orderInvariantIR() []cmp.Option {
	return []cmp.Option{
		cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash"),
		cmpopts.IgnoreFields(ir.Provenance{}, "Position"),
	}
}

// propertyOf returns the named component model's property with the given wire
// name.
func propertyOf(t *testing.T, doc *ir.Document, model, wire string) ir.Property {
	t.Helper()
	m, ok := doc.Types[componentID(model)].(*ir.Model)
	require.True(t, ok, "%s lowers to a model", model)
	p, ok := openapitest.PropsByWire(m.Properties)[wire]
	require.True(t, ok, "%s declares a property %q", model, wire)
	return p
}

// keywordCensusSpec wraps the two $ref targets every ref-site row aliases in a
// component block, so a row states only what it declares beside its $ref.
func keywordCensusSpec(schemas string) string {
	return openapitest.ComponentSpec("    BaseStr: {type: string}\n" +
		"    BaseObj: {type: object, properties: {a: {type: string}}}\n" + schemas)
}

// diagsAtPointer returns the diagnostics in diags with the exact code whose
// provenance is pointer.
func diagsAtPointer(diags []ir.Diagnostic, code, pointer string) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == code && d.Provenance.Pointer == jsontext.Pointer(pointer) {
			out = append(out, d)
		}
	}
	return out
}

// marshalForTest returns doc's canonical JSON, to count entries wherever they
// sit in it.
func marshalForTest(t *testing.T, doc *ir.Document) string {
	t.Helper()
	out, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(out)
}
