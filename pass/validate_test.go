package pass_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
