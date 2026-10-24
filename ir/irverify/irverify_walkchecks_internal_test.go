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
// so every walk over it is cut short. The model carries a well-formed name, so
// the naming check has something to reach before the value tree runs the walk
// out of budget.
func deepDoc() *ir.Document {
	v := ir.Value{Kind: ir.ValueNull}
	for range ir.MaxWalkDepth {
		v = ir.Value{Kind: ir.ValueList, List: []ir.Value{v}}
	}
	m := &ir.Model{
		ID:       "t/x/M",
		Name:     ir.Naming{Source: "M", Canonical: "m"},
		Examples: []ir.Example{{Value: &v}},
	}
	return &ir.Document{Types: ir.TypeRegistry{m.ID: m}}
}

// TestWalkChecks_EachReportsTruncation drives the flag every walking check owes
// its caller. checkNaming used to discard it (GitHub #55), which was benign only
// because the unpruned reference walk reaches at least as far as every pruning
// one and so trips the cap first — a coincidence, not a guarantee, and one that
// hid every name past the cap the day a pruning walk went deeper.
func TestWalkChecks_EachReportsTruncation(t *testing.T) {
	t.Parallel()
	doc := deepDoc()
	decls := readDeclarations(doc)
	for _, check := range walkChecks() {
		_, truncated := check(doc, decls)
		assert.True(t, truncated,
			"%s walked a document nested past the cap without reporting it", checkName(check))
	}
}

// TestVerify_ReportsTruncationOnce holds the other half: five walks over one
// too-deep document are one fact about the document, so Verify states it once
// rather than once per walk that noticed it.
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

// TestWalkChecks_NoWalkDropsItsTruncationFlag guards walkChecks against drift
// with two claims that leave a walk nowhere to drop the flag. A function that
// calls ir.WalkValues must return a bool, so the flag cannot die at the walk
// site; and a function shaped like a walking check must be in walkChecks, so no
// path wires it into Verify without folding its flag into a report.
//
// The populations overlap without coinciding (checkReferentialIntegrity walks
// through collectRefs, a walker that is no check), so each claim is asked of
// the functions it applies to rather than of one list.
func TestWalkChecks_NoWalkDropsItsTruncationFlag(t *testing.T) {
	t.Parallel()
	listed := map[string]bool{}
	for _, check := range walkChecks() {
		listed[checkName(check)] = true
	}

	violation, err := verifyDecls(t).Named("Violation")
	require.NoError(t, err)
	violations := types.NewSlice(violation)
	fns := packageFuncs(t)
	require.NotEmpty(t, fns, "the package must declare functions")
	var walkers int
	for _, fn := range fns {
		if fn.callsWalkValues {
			walkers++
			assert.True(t, endsInBool(fn.sig),
				"%s calls ir.WalkValues but returns no truncation flag", fn.name)
		}
		if isCheckShaped(fn.sig, violations) {
			assert.True(t, listed[fn.name],
				"%s is shaped like a walking check but is not in walkChecks, "+
					"so nothing folds its truncation flag into a report", fn.name)
		}
	}
	assert.Positive(t, walkers,
		"nothing reaches the document through ir.WalkValues, so the claim above proves nothing")
}

// checkName is a walkChecks entry's function name, without its package path.
// A name with no dot is returned whole.
func checkName(check func(*ir.Document, declarations) ([]Violation, bool)) string {
	full := runtime.FuncForPC(reflect.ValueOf(check).Pointer()).Name()
	if _, after, ok := strings.CutLast(full, "."); ok {
		return after
	}
	return full
}

// packageFunc is one function this package's production sources declare: its
// name, its signature, and whether its body reaches ir.WalkValues.
type packageFunc struct {
	name            string
	sig             *types.Signature
	callsWalkValues bool
}

// packageFuncs returns every function declared in this package's production
// sources, methods included.
func packageFuncs(t *testing.T) []packageFunc {
	t.Helper()
	p := verifyDecls(t)
	irAsSeen, reaches := p.Imported(irPath)
	require.True(t, reaches, "this package must import ir")
	walkValues := irAsSeen.Scope().Lookup("WalkValues")
	require.NotNil(t, walkValues, "the ir package must declare WalkValues")
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
			out = append(out, packageFunc{
				name:            fn.Name.Name,
				sig:             sig,
				callsWalkValues: refersTo(p.Info, fn, walkValues),
			})
		}
	}
	return out
}

// endsInBool reports whether sig's last result, named or not, has type bool.
func endsInBool(sig *types.Signature) bool {
	results := sig.Results()
	return results.Len() > 0 && types.Identical(results.At(results.Len()-1).Type(), types.Typ[types.Bool])
}

// isCheckShaped reports whether sig returns (violations, bool): the shape of a
// check that walks the document and owes its caller a truncation flag.
func isCheckShaped(sig *types.Signature, violations types.Type) bool {
	results := sig.Results()
	return results.Len() == 2 && endsInBool(sig) &&
		types.Identical(results.At(0).Type(), violations)
}

// refersTo reports whether fn's body names target, resolved through info so an
// import renamed or dotted still counts. target is compared as an object, so it
// must be the one info's package sees, as Imported reaches it.
func refersTo(info *types.Info, fn *ast.FuncDecl, target types.Object) bool {
	for node := range ast.Preorder(fn) {
		ident, isIdent := node.(*ast.Ident)
		if isIdent && info.Uses[ident] == target {
			return true
		}
	}
	return false
}
