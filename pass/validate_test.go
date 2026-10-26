package pass_test

import (
	"context"
	"encoding/json/v2"
	"math"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/ctxtest"
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

// validated is what pass.Validate reports about doc under the test's context,
// failing tb unless it finishes.
func validated(tb testing.TB, doc *ir.Document) []ir.Diagnostic {
	tb.Helper()
	diags, err := pass.Validate(tb.Context(), doc)
	require.NoError(tb, err)
	return diags
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
	assert.Empty(t, validated(t, validDoc()))
}

func TestValidate_NilDocumentReturnsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, validated(t, nil))
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
	assert.Empty(t, validated(t, doc))
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
	want := validated(t, doc)
	require.Contains(t, irtest.DiagCodes(want), "ir/dangling-type-ref")
	before, err := json.Marshal(doc)
	require.NoError(t, err)

	out, got := pass.Validator{}.Run(t.Context(), doc)

	assert.Equal(t, "validate", pass.Validator{}.Name(), "the name callers type")
	assert.Same(t, doc, out, "an analysis returns its input")
	assert.Empty(t, cmp.Diff(want, got))
	after, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(string(before), string(after)), "a pass never mutates its input")
}

// danglingDoc is validDoc with its one property referencing a type nothing
// declares, so a whole Validate reports it.
func danglingDoc() *ir.Document {
	doc := validDoc()
	doc.Types["t/m"] = &ir.Model{ID: "t/m", Properties: []ir.Property{
		{ID: "p/m/a", WireName: "a", Type: ir.TypeRef{Target: "t/missing"}},
	}}
	return doc
}

// TestValidate_ACancelAtAnyCheckReportsNothing cancels a Validate at every check
// of its context a whole one makes, one Validate per check, and requires the
// same end each time: the context's error, no finding, and no ask after the
// refusal. A whole Validate asks as its walk goes, between every two families
// and after the last, so a cancel waits out one family at most and a family or
// a walk cut short is never reported.
func TestValidate_ACancelAtAnyCheckReportsNothing(t *testing.T) {
	t.Parallel()
	doc := danglingDoc()
	whole := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
	diags, err := pass.Validate(whole, doc)
	require.NoError(t, err)
	require.NotEmpty(t, diags, "a whole Validate has findings a cut one could leak")
	require.Greater(t, whole.Calls, pass.Families()+1, "the walk asks too, not only the families")

	for left := range whole.Calls {
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}

		diags, err := pass.Validate(ctx, doc)

		require.ErrorIs(t, err, context.Canceled, "cancelled at check %d of %d", left+1, whole.Calls)
		require.Nil(t, diags, "check %d", left+1)
		require.Equal(t, left+1, ctx.Calls, "check %d: asked on after the refusal", left+1)
	}
}

// TestValidator_RunReportsNothingOnceItsContextIsDone pins the adapter under a
// done context: it hands back the document it was given, with no finding,
// since what Validate found before it stopped is not all it would find.
func TestValidator_RunReportsNothingOnceItsContextIsDone(t *testing.T) {
	t.Parallel()
	doc := danglingDoc()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	out, got := pass.Validator{}.Run(ctx, doc)

	assert.Same(t, doc, out)
	assert.Nil(t, got)
}

// ctxKey keys the value TestValidate_WalksTheDocumentOnce puts on a context.
type ctxKey struct{}

// TestValidate_WalksTheDocumentOnce counts the walks Validate makes through the
// walk validate is handed: one, carrying what its three walking checks read,
// under Validate's context. No result can show it, since a walk per check
// reports the same. The counted run must report what Validate does, so the
// count is of the real run, and the document carries a dangling reference so
// that run reports something.
func TestValidate_WalksTheDocumentOnce(t *testing.T) {
	t.Parallel()
	doc := danglingDoc()
	ctx := context.WithValue(t.Context(), ctxKey{}, "validate's")
	var walks, visitors int
	var under any
	got, err := pass.ValidateWith(ctx, doc, func(ctx context.Context, root any, path string, vs ...ir.Visitor) (bool, error) {
		walks++
		visitors = len(vs)
		under = ctx.Value(ctxKey{})
		return ir.WalkValuesContext(ctx, root, path, vs...)
	})
	require.NoError(t, err)

	assert.Equal(t, 1, walks, "the walking checks must share one walk of the document")
	assert.Equal(t, 2, visitors, "the walk carries the references with what they resolve against, and the PropIDs")
	assert.Equal(t, "validate's", under, "the walk runs under Validate's context")
	assert.Contains(t, irtest.DiagCodes(got), "ir/dangling-type-ref")
	assert.Empty(t, cmp.Diff(validated(t, doc), got), "the counted run reports what Validate does")
}

// TestValidate_WalksOnlyThroughTheSharedWalk holds the other half, which counting
// cannot see: a check that walked the document itself would not go through the
// walk validate is handed. Of the reflection walks ir offers, the pass may name
// ir.WalkValuesContext only in Validate, which hands it on, and ir.WalkValues
// only in appendTypeIDs, which walks one GraphQL operation rather than the
// document.
func TestValidate_WalksOnlyThroughTheSharedWalk(t *testing.T) {
	t.Parallel()
	p, err := decls.Load(reflect.TypeFor[pass.Validator]().PkgPath())
	require.NoError(t, err)
	irAsSeen, reaches := p.Imported(reflect.TypeFor[ir.Document]().PkgPath())
	require.True(t, reaches, "the pass must import ir")
	for name, want := range map[string][]string{"WalkValuesContext": {"Validate"}, "WalkValues": {"appendTypeIDs"}} {
		walk := irAsSeen.Scope().Lookup(name)
		require.NotNil(t, walk, "the ir package must declare %s", name)
		assert.Equal(t, want, p.FuncsUsing(walk), "the functions that walk through ir.%s", name)
	}
}
