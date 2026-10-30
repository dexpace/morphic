// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"go/ast"
	"go/types"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// deepDoc returns a document whose one example value nests past ir.MaxWalkDepth,
// so a walk over it is cut short.
func deepDoc() *ir.Document {
	v := ir.Value{Kind: ir.ValueNull}
	for range ir.MaxWalkDepth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	m := &ir.Model{ID: "t/x/M", Examples: []ir.Example{{Value: &v}}}
	return &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
}

// TestVerify_ReportsTruncationOnce holds Verify to stating a cut-short walk once:
// it is one fact about the document, however many of the checks the walk carries
// reached past the cap.
func TestVerify_ReportsTruncationOnce(t *testing.T) {
	t.Parallel()
	var truncations int
	for _, v := range Verify(deepDoc()) {
		if v.Code != "ir/walk-truncated" {
			continue
		}
		truncations++
		assert.Equal(t, "doc", v.Path, "truncation is a fact about the document, not about a node")
	}
	assert.Equal(t, 1, truncations)
}

// runCheck runs the one walking check newCheck over doc as Verify runs every
// check: in one walk that reads the declarations too. It returns what the check
// reports and whether the walk was cut short.
func runCheck(newCheck func(*ir.Document) walkCheck, doc *ir.Document) ([]Violation, bool) {
	return walkWith(doc, ir.WalkValues, []func(*ir.Document) walkCheck{newCheck})
}

// TestVerify_WalksTheDocumentOnce counts the walks Verify makes, through the walk
// verify is handed: one, carrying the declarations and every walking check. It
// is what holds the walking checks to sharing one traversal, which no result can
// show, since running each in a walk of its own reports the same. The counted
// run must also report what Verify does, so the count is of the real run.
func TestVerify_WalksTheDocumentOnce(t *testing.T) {
	t.Parallel()
	doc := deepDoc()
	var walks, visitors int
	got := verify(doc, func(root any, path string, vs ...ir.Visitor) bool {
		walks++
		visitors = len(vs)
		return ir.WalkValues(root, path, vs...)
	})

	assert.Equal(t, 1, walks, "every walking check must share one walk of the document")
	assert.Equal(t, 1+len(walkChecks()), visitors, "the walk carries the declarations and every walking check")
	assert.Equal(t, Verify(doc), got, "the counted run reports what Verify does")
}

// TestWalkWith_ReportsInTheOrderGiven pins the order walkWith concatenates the
// reports in, which Verify's sort leaves visible only where two checks place one
// code at one path.
func TestWalkWith_ReportsInTheOrderGiven(t *testing.T) {
	t.Parallel()
	reportingOnly := func(code string) func(*ir.Document) walkCheck {
		return func(*ir.Document) walkCheck {
			return walkCheck{report: func(declarations) []Violation { return []Violation{{Code: code}} }}
		}
	}
	got, truncated := walkWith(&ir.Document{}, ir.WalkValues,
		[]func(*ir.Document) walkCheck{reportingOnly("second"), reportingOnly("first")})
	assert.False(t, truncated)
	assert.Equal(t, []Violation{{Code: "second"}, {Code: "first"}}, got)
}

// TestVerify_WalksOnlyThroughTheSharedWalk holds the other half, which counting
// cannot see: a check that walked the document itself would not go through the
// walk verify is handed. Of the traversals ir offers, irverify's production
// code may name only ir.WalkValues, and only in Verify, which hands it on.
func TestVerify_WalksOnlyThroughTheSharedWalk(t *testing.T) {
	t.Parallel()
	p := verifyDecls(t)
	irAsSeen, reaches := p.Imported(irPath)
	require.True(t, reaches, "this package must import ir")
	for traversal, want := range map[string][]string{
		"WalkValues":       {"Verify"},
		"ForEachOperation": nil,
		"TypeEdges":        nil,
	} {
		obj := irAsSeen.Scope().Lookup(traversal)
		require.NotNil(t, obj, "the ir package must declare %s", traversal)
		assert.Equal(t, want, p.FuncsUsing(obj), "the functions that walk the document through ir.%s", traversal)
	}
}

// TestWalkChecks_ListsEveryWalkingCheck holds walkChecks to every function shaped
// like a walking check, so none is written and left out of the walk, where it
// would check nothing.
func TestWalkChecks_ListsEveryWalkingCheck(t *testing.T) {
	t.Parallel()
	listed := map[string]bool{}
	for _, check := range walkChecks() {
		listed[checkName(check)] = true
	}
	shape, err := verifyDecls(t).Named("walkCheck")
	require.NoError(t, err)
	var shaped int
	for _, fn := range packageFuncs(t) {
		if !isCheckShaped(fn.sig, shape) {
			continue
		}
		shaped++
		assert.True(t, listed[fn.name], "%s is shaped like a walking check but is not in walkChecks", fn.name)
	}
	assert.Len(t, listed, shaped, "walkChecks lists each check once, and only functions shaped like one")
}

// checkName is a walkChecks entry's function name, without its package path.
// A name with no dot is returned whole.
func checkName(check func(*ir.Document) walkCheck) string {
	full := runtime.FuncForPC(reflect.ValueOf(check).Pointer()).Name()
	if _, after, ok := strings.CutLast(full, "."); ok {
		return after
	}
	return full
}

// packageFunc is one function this package's production sources declare: its
// name and its signature.
type packageFunc struct {
	name string
	sig  *types.Signature
}

// packageFuncs returns every function declared in this package's production
// sources, methods included.
func packageFuncs(t *testing.T) []packageFunc {
	t.Helper()
	p := verifyDecls(t)
	var out []packageFunc
	for _, f := range p.Syntax {
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc {
				continue
			}
			obj, isFuncObj := p.Info.Defs[fn.Name].(*types.Func)
			require.True(t, isFuncObj, "%s declares no function object", fn.Name.Name)
			sig, isSig := obj.Type().(*types.Signature)
			require.True(t, isSig, "%s has no signature", fn.Name.Name)
			out = append(out, packageFunc{name: fn.Name.Name, sig: sig})
		}
	}
	require.NotEmpty(t, out, "the package must declare functions")
	return out
}

// isCheckShaped reports whether sig is a walking check's: it takes the document
// and returns a walkCheck.
func isCheckShaped(sig *types.Signature, shape types.Type) bool {
	params, results := sig.Params(), sig.Results()
	if sig.Recv() != nil || params.Len() != 1 || results.Len() != 1 {
		return false
	}
	doc, isPtr := params.At(0).Type().(*types.Pointer)
	return isPtr && types.TypeString(doc.Elem(), nil) == irPath+".Document" &&
		types.Identical(results.At(0).Type(), shape)
}
