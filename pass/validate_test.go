// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
			"type/primitive/string": &ir.Primitive{ID: "type/primitive/string", PrimitiveKind: "string"},
			"type/m": &ir.Model{ID: "type/m", Properties: []ir.Property{
				{ID: "property/m/a", WireName: "a", Type: ir.TypeRef{Target: "type/primitive/string"}},
			}},
		},
	}
}

// determinismRuns is how many times validated reads a document that drew two
// or more findings. A Go map of at most eight entries is ranged from a random
// slot in a fixed slot order, a rotation and no shuffle, so two findings from
// two keys swap on about one run in eight: measured over maps of 2, 4 and 8
// entries, every one of 16 runs agreed in about one trial in nine, and no
// trial of 128 did, over 64 entries as well. A check ranging a map in place of
// its sorted keys therefore reddens whatever test reads it through validated.
const determinismRuns = 128

// fataler is what readRepeatedly needs of its test, so a test can hand it one
// that records the failure.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

// validatedOnce is what pass.Validate reports about doc under the test's
// context, failing tb unless it finishes. It reads the document once: a test
// that measures reads it so.
func validatedOnce(tb testing.TB, doc *ir.Document) []ir.Diagnostic {
	tb.Helper()
	diags, err := pass.Validate(tb.Context(), doc)
	require.NoError(tb, err)
	return diags
}

// validated is what pass.Validate reports about doc, read until it has said so
// determinismRuns times when it draws two findings or more, so a finding order
// that follows a map's range fails whichever test read it (GitHub #1546). A
// document drawing fewer is read once: with one finding there is no order to
// vary. It fails tb unless every reading is the first.
func validated(tb testing.TB, doc *ir.Document) []ir.Diagnostic {
	tb.Helper()
	return readRepeatedly(tb, func() []ir.Diagnostic { return validatedOnce(tb, doc) })
}

// readRepeatedly reads once, and again determinismRuns-1 times if that drew two
// findings or more, failing tb on the first reading that differs from the
// first, and returns the first.
func readRepeatedly(tb fataler, read func() []ir.Diagnostic) []ir.Diagnostic {
	tb.Helper()
	first := read()
	if len(first) < 2 {
		return first
	}
	for range determinismRuns - 1 {
		if diff := cmp.Diff(first, read()); diff != "" {
			tb.Fatalf("Validate reported one document two ways (-first +again):\n%s", diff)
		}
	}
	return first
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
	operationGroup := ir.OperationGroup{Operations: []ir.Operation{op}}
	for range depth {
		operationGroup = ir.OperationGroup{Groups: []ir.OperationGroup{operationGroup}}
	}
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{operationGroup}}}
	return doc
}

// countCode returns how many of diags carry the given diagnostic code.
func countCode(t *testing.T, diags []ir.Diagnostic, code string) int {
	t.Helper()
	var n int
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			n++
		}
	}
	return n
}

// messageForCode returns the message of the single diagnostic carrying code.
func messageForCode(t *testing.T, diags []ir.Diagnostic, code string) string {
	t.Helper()
	require.Equal(t, 1, countCode(t, diags, code), "expected exactly one %s", code)
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			return diagnostic.Message
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
	doc.Types["type/m"] = &ir.Model{ID: "type/m", Properties: []ir.Property{
		{ID: "property/m/a", WireName: "a", Type: ir.TypeRef{Target: "type/missing"}},
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
	doc.Types["type/m"] = &ir.Model{ID: "type/m", Properties: []ir.Property{
		{ID: "property/m/a", WireName: "a", Type: ir.TypeRef{Target: "type/missing"}},
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
// walk validate is handed: one, carrying what its four walking checks read,
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
	assert.Equal(t, 3, visitors,
		"the walk carries the references with what they resolve against, the PropIDs and the empty references")
	assert.Equal(t, "validate's", under, "the walk runs under Validate's context")
	assert.Contains(t, irtest.DiagCodes(got), "ir/dangling-type-ref")
	assert.Empty(t, cmp.Diff(validated(t, doc), got), "the counted run reports what Validate does")
}

// TestValidate_WalksOnlyThroughTheSharedWalk holds the other half, which counting
// cannot see: a check that walked the document itself would not go through the
// walk validate is handed. Of the reflection walks ir offers, the package may
// name ir.WalkValuesContext only in Validate, which hands it on, and in
// ConstUnions.Run, a pass of its own, which counts the references to the
// branches it may remove before validate runs, and ir.WalkValues only in
// appendTypeIDs, which walks one GraphQL operation rather than the document.
func TestValidate_WalksOnlyThroughTheSharedWalk(t *testing.T) {
	t.Parallel()
	loadedPackage, err := decls.Load(reflect.TypeFor[pass.Validator]().PkgPath())
	require.NoError(t, err)
	irAsSeen, reaches := loadedPackage.Imported(reflect.TypeFor[ir.Document]().PkgPath())
	require.True(t, reaches, "the pass must import ir")
	for name, want := range map[string][]string{
		"WalkValuesContext": {"Run", "Validate"}, "WalkValues": {"appendTypeIDs"},
	} {
		walk := irAsSeen.Scope().Lookup(name)
		require.NotNil(t, walk, "the ir package must declare %s", name)
		assert.Equal(t, want, loadedPackage.FuncsUsing(walk), "the functions that walk through ir.%s", name)
	}
}
