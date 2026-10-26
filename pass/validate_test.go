package pass_test

import (
	"encoding/json/v2"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pass"
)

// validDoc returns a minimal internally consistent document for a case to
// mutate. Keep it tiny: one model referencing one primitive.
func validDoc() *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion, Name: "t", Version: "1",
		Types: ir.TypeRegistry{
			"t/prim/string": &ir.Primitive{ID: "t/prim/string", Prim: "string"},
			"t/m": &ir.Model{ID: "t/m", Properties: []ir.Property{
				{ID: "p/m/a", WireName: "a", Type: ir.TypeRef{Target: "t/prim/string"}},
			}},
		},
	}
}

// docWithOperation wraps a single operation in a service/group so the operation
// walkers reach it.
func docWithOperation(op ir.Operation) *ir.Document {
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID:     "s",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{op}}},
	}}
	return doc
}

// docWithOperations wraps ops in one service and group.
func docWithOperations(ops ...ir.Operation) *ir.Document {
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{{Operations: ops}}}}
	return doc
}

// nestGroups buries op under depth levels of operation groups.
func nestGroups(depth int, op ir.Operation) *ir.Document {
	g := ir.OperationGroup{Operations: []ir.Operation{op}}
	for range depth {
		g = ir.OperationGroup{Groups: []ir.OperationGroup{g}}
	}
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{g}}}
	return doc
}

// countCode returns how many of diags carry the given diagnostic code.
func countCode(t *testing.T, diags []ir.Diagnostic, code string) int {
	t.Helper()
	var n int
	for _, d := range diags {
		if d.Code == code {
			n++
		}
	}
	return n
}

// messageForCode returns the message of the single diagnostic carrying code.
func messageForCode(t *testing.T, diags []ir.Diagnostic, code string) string {
	t.Helper()
	require.Equal(t, 1, countCode(t, diags, code), "expected exactly one %s", code)
	for _, d := range diags {
		if d.Code == code {
			return d.Message
		}
	}
	return ""
}

func TestValidate_CleanDocumentHasNoDiagnostics(t *testing.T) {
	t.Parallel()
	assert.Empty(t, pass.Validate(validDoc()))
}

func TestValidate_NilDocumentReturnsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, pass.Validate(nil))
}

func TestValidate_SharedRouteIsLegal(t *testing.T) {
	t.Parallel()
	mk := func() ir.Operation {
		return ir.Operation{Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x", SharedRoute: true,
		}}}}
	}
	doc := validDoc()
	doc.Services = []ir.Service{{
		ID:     "s",
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{mk(), mk()}}},
	}}
	assert.Empty(t, pass.Validate(doc))
}

// TestValidator_RunHandsBackItsInputWithValidatesFindings pins the adapter: it
// is named validate, returns the document it was given untouched, and reports
// exactly what Validate does. The document carries a dangling ref, so a Run
// that dropped the findings could not pass.
func TestValidator_RunHandsBackItsInputWithValidatesFindings(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/m"] = &ir.Model{ID: "t/m", Properties: []ir.Property{
		{ID: "p/m/a", WireName: "a", Type: ir.TypeRef{Target: "t/missing"}},
	}}
	want := pass.Validate(doc)
	require.Contains(t, irtest.DiagCodes(want), "ir/dangling-type-ref")
	before, err := json.Marshal(doc)
	require.NoError(t, err)

	out, got := pass.Validator{}.Run(doc)

	assert.Equal(t, "validate", pass.Validator{}.Name(), "the name callers type")
	assert.Same(t, doc, out, "an analysis returns its input")
	assert.Empty(t, cmp.Diff(want, got))
	after, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(string(before), string(after)), "a pass never mutates its input")
}

// TestValidate_WalksTheDocumentOnce counts the walks Validate makes through the
// walk validate is handed: one, carrying what its three walking checks read. No
// result can show it, since a walk per check reports the same. The counted run
// must report what Validate does, so the count is of the real run, and the
// document carries a dangling reference so that run reports something.
func TestValidate_WalksTheDocumentOnce(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["t/m"] = &ir.Model{ID: "t/m", Properties: []ir.Property{
		{ID: "p/m/a", WireName: "a", Type: ir.TypeRef{Target: "t/missing"}},
	}}
	var walks, visitors int
	got := pass.ValidateWith(doc, func(root any, path string, vs ...ir.Visitor) bool {
		walks++
		visitors = len(vs)
		return ir.WalkValues(root, path, vs...)
	})

	assert.Equal(t, 1, walks, "the walking checks must share one walk of the document")
	assert.Equal(t, 3, visitors, "the walk carries the declarations, the references and the PropIDs")
	assert.Contains(t, irtest.DiagCodes(got), "ir/dangling-type-ref")
	assert.Empty(t, cmp.Diff(pass.Validate(doc), got), "the counted run reports what Validate does")
}

// TestValidate_WalksOnlyThroughTheSharedWalk holds the other half, which counting
// cannot see: a check that walked the document itself would not go through the
// walk validate is handed. Of the reflection walks ir offers, the pass may name
// ir.WalkValues only in Validate, which hands it on, and in appendTypeIDs, which
// walks one GraphQL operation rather than the document.
func TestValidate_WalksOnlyThroughTheSharedWalk(t *testing.T) {
	t.Parallel()
	p, err := decls.Load(reflect.TypeFor[pass.Validator]().PkgPath())
	require.NoError(t, err)
	irAsSeen, reaches := p.Imported(reflect.TypeFor[ir.Document]().PkgPath())
	require.True(t, reaches, "the pass must import ir")
	for traversal, want := range map[string][]string{
		"WalkValues": {"Validate", "appendTypeIDs"},
	} {
		obj := irAsSeen.Scope().Lookup(traversal)
		require.NotNil(t, obj, "the ir package must declare %s", traversal)
		assert.Equal(t, want, p.FuncsUsing(obj), "the functions that walk through ir.%s", traversal)
	}
}
