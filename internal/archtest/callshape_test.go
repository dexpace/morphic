// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/ssa"

	"github.com/dexpace/morphic/ir/irtest"
)

// callShapeBaseline is how many calls each production package, keyed by its
// path below the module, makes into each other one it does not import, directly
// or through others. Only a function or an interface handed to the caller can
// make such a call, and no import rule sees it. TestCallShape_HeldToTheBaseline fails when a
// count rises, and when one falls until it is lowered here, so the table only
// shrinks (GitHub #1478). A seam added by design, as a second compiler would be,
// adds its count the way an allowlist entry is added. Each entry says what its
// calls are.
var callShapeBaseline = map[string]map[string]int{
	"compilers": {
		// The registry calls the methods of each compiler it holds: a designed seam.
		"compilers/openapi": 5,
		// Detection's configuration, which the engine and the harness hand the
		// registry as a callback until #1482 makes it data.
		"engine":           1,
		"internal/harness": 1,
	},
	"compilers/compile": {
		// The settlement queue runs what a lowering deferred, once the registry is
		// whole and in order (#898): a designed seam. Types.Intern also calls the
		// build schema hands it before it returns: an immediate callback.
		"compilers/openapi/internal/operation": 2,
		"compilers/openapi/internal/schema":    4,
	},
	"compilers/openapi/internal/annotation": {
		// Placement through lowering.Ctx.ProvenanceAt and schema's unlocated,
		// which #1485 replaces with one provenance value.
		"compilers/openapi/internal/lowering": 14,
		"compilers/openapi/internal/schema":   1,
	},
	"compilers/openapi/internal/bind": {
		// bind.Config's six functions, which #1488 keeps: I/O (Open), policy bind
		// may not import (Locate, Refuse), a live query (Written), and lookups
		// through load's key normalization (Through, Beside).
		"compilers/openapi/internal/load": 6,
	},
	"compilers/openapi/internal/load": {
		// DocumentReader, through which load reads the documents references and
		// overlays name: I/O at a designed seam.
		"engine":        2,
		"engine/reader": 6,
	},
	"compilers/openapi/internal/lowering": {
		// KeptOnce.Once, Promoter.Referent, Ctx.TypePosition and Remembered each
		// call the function they are handed before they return: immediate
		// callbacks.
		"compilers/openapi/internal/operation": 1,
		"compilers/openapi/internal/schema":    15,
	},
	"compilers/openapi/internal/merge": {
		// The allOf merge relay, which #1486 lowers to the fold's calls back into
		// the walk that declared the field.
		"compilers/openapi/internal/schema": 24,
	},
	"compilers/openapi/internal/nodeview": {
		// What nodeview.Walk calls as it visits and leaves a node: traversal
		// callbacks.
		"compilers/openapi/internal/load":        7,
		"compilers/openapi/internal/oas":         2,
		"compilers/openapi/internal/overlay":     3,
		"compilers/openapi/internal/sourceindex": 2,
	},
	"compilers/openapi/internal/oas": {
		// oas.Options' Locate and Seen, which #1488 keeps: the checker places its
		// own findings to drop the repeat an alias makes, and Seen observes the
		// walk.
		"compilers/openapi/internal/load": 9,
	},
	"compilers/openapi/internal/resolve": {
		// resolve.Scope's Holds, Beside and Writes, which #1488 keeps: a lazily
		// built index, and queries of load's index of the fields written beside a
		// path item $ref.
		"compilers/openapi/internal/load": 3,
		// Scope.Declares, filled with lowering.Ctx.DeclaresSchema until #1481 hands
		// it the names as a value.
		"compilers/openapi/internal/lowering": 1,
	},
	"compilers/openapi/internal/scan": {
		// Placement through scan.Locator and its pointers, which #1487 replaces
		// with one provenance tree.
		"compilers/openapi/internal/load": 11,
	},
	"engine/internal/atomicfile": {
		// Replace calls the fill it is handed before it returns: an immediate
		// callback.
		"engine/cache":  1,
		"engine/output": 1,
	},
	"ir": {
		// ir.Visitor and the ForEach walks: traversal callbacks.
		"internal/census": 1,
		"ir/irfacts":      2,
		"ir/irtest":       2,
		"ir/irverify":     2,
		"pass":            13,
	},
	"ir/irtest": {
		// AllocatingVisits runs the visitors it is handed: traversal callbacks. The
		// analysis reads one []ir.Visitor as any other, so it counts here each
		// visitor the module hands a variadic walk, though only tests call this.
		"ir/irverify": 3,
		"pass":        2,
	},
}

// callShapeRatchet is how a difference from callShapeBaseline reads.
var callShapeRatchet = ratchet{
	name:   "callShapeBaseline",
	counts: callShapeBaseline,
	what:   "calls into",
	rise: "each reaches a package the caller's does not import, which no import rule sees; hand the " +
		"caller a value instead, or, for a seam by design, raise the count with its reason (GitHub #1488)",
}

// TestCallShape_HeldToTheBaseline holds the calls each production package makes
// into packages it does not import to callShapeBaseline. A run that analyzed
// nothing finds no call, and fails each count the table holds. A count does not
// say which calls make it up, so a call removed and another added in one pair
// leave it as it was.
func TestCallShape_HeldToTheBaseline(t *testing.T) {
	t.Parallel()
	p := moduleProgram(t)
	calls, err := reversedCalls(p)
	require.NoError(t, err)

	root := irtest.RepoRoot(t)
	sites := make(map[string]map[string][]string, len(calls))
	for caller, byCallee := range calls {
		sites[caller] = make(map[string][]string, len(byCallee))
		for callee, at := range byCallee {
			for _, c := range at {
				sites[caller][callee] = append(sites[caller][callee], relPosition(c.at, root)+" calls "+c.callee)
			}
		}
	}
	for _, finding := range baselineFindings(callShapeRatchet, sites) {
		t.Error(finding)
	}
}

// reversedCall is one call from a package into one it does not import: where
// the call is, and the function it reaches, named within its package.
type reversedCall struct {
	at     token.Position
	callee string
}

// reversedCalls returns each call p's call graph holds from one of its packages
// into another that the caller's does not import, directly or through others,
// keyed by the two packages' names in p.own. A call is a call site and a
// function it reaches, counted once however many instances of a generic
// function hold either. Calls made by callRunner, protocol calls and the bodies
// of range-over-func loops are left out.
func reversedCalls(p program) (map[string]map[string][]reversedCall, error) {
	graph := callGraph(p)
	imports := importsOf(p.own)

	type key struct {
		site   token.Pos
		callee *ssa.Function
	}
	seen := map[key]bool{}
	out := map[string]map[string][]reversedCall{}
	err := callgraph.GraphVisitEdges(graph, func(e *callgraph.Edge) error {
		caller, callee := declared(e.Caller.Func), declared(e.Callee.Func)
		if e.Site == nil || caller.Pkg == nil || callee.Pkg == nil {
			return fmt.Errorf("the call graph holds a call from %s to %s that no source makes", e.Caller.Func, e.Callee.Func)
		}
		from, ownCaller := p.own[caller.Pkg]
		to, ownCallee := p.own[callee.Pkg]
		if !ownCaller || !ownCallee || caller.Pkg == callee.Pkg || imports[caller.Pkg][callee.Pkg.Pkg] ||
			from == callRunner || protocolCall(e.Site) || rangeBody(callee) {
			return nil
		}
		k := key{e.Site.Pos(), callee}
		if !k.site.IsValid() {
			return fmt.Errorf("%s calls %s at no position in the source, so the call cannot be counted once",
				e.Caller.Func, e.Callee.Func)
		}
		if seen[k] {
			return nil
		}
		seen[k] = true
		if out[from] == nil {
			out[from] = map[string][]reversedCall{}
		}
		out[from][to] = append(out[from][to], reversedCall{p.ssa.Fset.Position(k.site), callee.RelString(callee.Pkg.Pkg)})
		return nil
	})
	for _, byCallee := range out {
		for _, calls := range byCallee {
			slices.SortFunc(calls, func(a, b reversedCall) int {
				return cmp.Or(cmp.Compare(a.at.Filename, b.at.Filename), cmp.Compare(a.at.Offset, b.at.Offset),
					cmp.Compare(a.callee, b.callee))
			})
		}
	}
	return out, err
}

// callGraph returns the call graph of what p's packages can run: variable type
// analysis over the functions rapid type analysis reaches from each function
// they declare and each package's initializer, seeded with the graph rapid type
// analysis builds. ssautil.AllFunctions is no basis: it answers differently from
// call to call on one program, and a wrapper it leaves out takes its calls with
// it. No generic function is a root, as rapid type analysis cannot start from
// one; its instances are reached from their callers.
func callGraph(p program) *callgraph.Graph {
	roots := make([]*ssa.Function, 0, len(p.funcs)+len(p.own))
	for _, f := range p.funcs {
		if f.fn.TypeParams().Len() == 0 {
			roots = append(roots, f.fn)
		}
	}
	for _, pkg := range slices.SortedFunc(maps.Keys(p.own), func(a, b *ssa.Package) int {
		return cmp.Compare(a.Pkg.Path(), b.Pkg.Path())
	}) {
		roots = append(roots, pkg.Func("init"))
	}
	reached := rta.Analyze(roots, true)
	funcs := make(map[*ssa.Function]bool, len(reached.Reachable))
	for fn := range reached.Reachable {
		funcs[fn] = true
	}
	graph := vta.CallGraph(funcs, reached.CallGraph)
	graph.DeleteSyntheticNodes()
	return graph
}

// callRunner is the package whose calls are not counted: barrier runs the code
// its caller hands it, so each call it makes into another package runs back up.
const callRunner = "barrier"

// importsOf returns, for each package of own, the packages it imports, directly
// or through others.
func importsOf(own map[*ssa.Package]string) map[*ssa.Package]map[*types.Package]bool {
	out := make(map[*ssa.Package]map[*types.Package]bool, len(own))
	for p := range own {
		reached := map[*types.Package]bool{}
		queue := slices.Clone(p.Pkg.Imports())
		for len(queue) > 0 {
			q := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			if !reached[q] {
				reached[q] = true
				queue = append(queue, q.Imports()...)
			}
		}
		out[p] = reached
	}
	return out
}

// declared returns the function the source declares for fn: the generic
// function of an instance, an anonymous one inside an instance included, and fn
// itself otherwise. Its Pkg is the package declaring it, which an anonymous
// function shares with the one enclosing it and a synthetic wrapper lacks.
func declared(fn *ssa.Function) *ssa.Function {
	if origin := fn.Origin(); origin != nil {
		return origin
	}
	return fn
}

// protocolCall reports whether site calls, through an interface, a method of
// one of the standard library's protocols, which a type anywhere may implement:
// Error, String, Format, GoString and Unwrap.
func protocolCall(site ssa.CallInstruction) bool {
	call := site.Common()
	if !call.IsInvoke() {
		return false
	}
	switch call.Method.Name() {
	case "Error", "String", "Format", "GoString", "Unwrap":
		return true
	default:
		return false
	}
}

// rangeBody reports whether fn is the body of a range-over-func loop, which
// the iterator calls as a for statement runs its body.
func rangeBody(fn *ssa.Function) bool {
	_, isRange := fn.Syntax().(*ast.RangeStmt)
	return isRange
}

// plantedPackage is one package plantedPackages builds: its import path, which
// is also its name in the result, and its one file's source.
type plantedPackage struct {
	path, src string
}

// plantedPackages builds each of srcs as a package of its own, in order, so
// that one may import any before it by its path, and returns them as the
// call-shape check reads a program.
func plantedPackages(t *testing.T, srcs ...plantedPackage) program {
	t.Helper()
	fset := token.NewFileSet()
	prog := ssa.NewProgram(fset, ssa.InstantiateGenerics)
	p := program{ssa: prog, own: map[*ssa.Package]string{}}
	built := map[string]*types.Package{}
	for _, src := range srcs {
		f, err := parser.ParseFile(fset, src.path+".go", src.src, parser.SkipObjectResolution)
		require.NoError(t, err)
		files, info := []*ast.File{f}, newInfo()
		conf := types.Config{Importer: importerFunc(func(path string) (*types.Package, error) {
			if imported, ok := built[path]; ok {
				return imported, nil
			}
			return nil, fmt.Errorf("%s imports %s, which is not planted before it", src.path, path)
		})}
		checked, err := conf.Check(src.path, fset, files, info)
		require.NoError(t, err, "the plant must type-check, or it proves nothing")
		built[src.path] = checked
		p.own[prog.CreatePackage(checked, files, info, true)] = src.path
		funcs, err := declaredFuncs(prog, src.path, files, info)
		require.NoError(t, err)
		p.funcs = append(p.funcs, funcs...)
	}
	prog.Build()
	return p
}

// callCounts returns how many calls calls holds for each pair of packages.
func callCounts(calls map[string]map[string][]reversedCall) map[string]map[string]int {
	out := make(map[string]map[string]int, len(calls))
	for caller, byCallee := range calls {
		out[caller] = make(map[string]int, len(byCallee))
		for callee, at := range byCallee {
			out[caller][callee] = len(at)
		}
	}
	return out
}

// TestCallShape_CountsAPlantedRelay plants the shape the baseline exists for:
// lower stores a function and calls it later, and upper, which imports lower,
// hands it a closure. The call lower makes counts against the import direction,
// and upper's own call into lower does not.
func TestCallShape_CountsAPlantedRelay(t *testing.T) {
	t.Parallel()
	calls, err := reversedCalls(plantedPackages(t,
		plantedPackage{"lower", `package lower
type Index struct{ later func() }
func New(later func()) *Index { return &Index{later: later} }
func (x *Index) Settle() { x.later() }`},
		plantedPackage{"upper", `package upper
import "lower"
var settled int
func Plant() { lower.New(func() { settled++ }).Settle() }`},
	))
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]int{"lower": {"upper": 1}}, callCounts(calls))
}

// TestCallShape_CountsEachWayACallRunsBack plants, in a package lower and one
// importing it, upper, each way a call runs back against the import direction,
// and the calls between them the count leaves out.
func TestCallShape_CountsEachWayACallRunsBack(t *testing.T) {
	t.Parallel()
	none := map[string]map[string]int{}
	once := map[string]map[string]int{"lower": {"upper": 1}}
	tests := []struct {
		name         string
		lower, upper string
		want         map[string]map[string]int
	}{
		{"a method value stored and called later", `package lower
var held func() int
func Hold(f func() int) { held = f }
func Run() int { return held() }`, `package upper
import "lower"
type T struct{}
func (T) n() int { return 1 }
func Plant() { lower.Hold(T{}.n); _ = lower.Run() }`, once},
		{"an interface handed down and called", `package lower
type Visitor interface{ Visit() }
func Walk(v Visitor) { v.Visit() }`, `package upper
import "lower"
type v struct{}
func (v) Visit() {}
func Plant() { lower.Walk(v{}) }`, once},
		{"a method promoted from an embedded type, through its wrapper", `package lower
type Visitor interface{ Visit() }
func Walk(v Visitor) { v.Visit() }`, `package upper
import "lower"
type inner struct{}
func (inner) Visit() {}
type outer struct{ inner }
func Plant() { lower.Walk(&outer{}) }`, once},
		{"one site reaching two functions counts each", `package lower
func Run(f func()) { f() }`, `package upper
import "lower"
func a() {}
func b() {}
func Plant() { lower.Run(a); lower.Run(b) }`, map[string]map[string]int{"lower": {"upper": 2}}},
		{"a site of a generic function counts once for a function many instances reach", `package lower
func Apply[T any](v T, f func()) { f() }`, `package upper
import "lower"
func done() {}
func Plant() { lower.Apply(1, done); lower.Apply("", done) }`, once},
		{"a generic function reached through two instances counts once", `package lower
func Run(f func()) { f() }`, `package upper
import "lower"
func each[T any]() {}
func Plant() { lower.Run(each[int]); lower.Run(each[string]) }`, once},
		{"a site in a closure of a generic function", `package lower
func Apply[T any](f func()) { func() { f() }() }`, `package upper
import "lower"
func done() {}
func Plant() { lower.Apply[int](done) }`, once},
		{"a function a package initializer hands down", `package lower
var held func()
func Hold(f func()) bool { held = f; return true }
func Run() { held() }`, `package upper
import "lower"
var _ = lower.Hold(func() {})`, once},
		{"a String method value is counted: only an interface call of one is a protocol call", `package lower
var held func() string
func Hold(f func() string) { held = f }
func Run() string { return held() }`, `package upper
import "lower"
type T struct{}
func (T) String() string { return "" }
func Plant() { lower.Hold(T{}.String); _ = lower.Run() }`, once},
		{"a protocol method called through an interface is not counted", `package lower
func Describe(err error) string { return err.Error() }`, `package upper
import "lower"
type failure struct{}
func (failure) Error() string { return "" }
func Plant() string { return lower.Describe(failure{}) }`, none},
		{"the body of a range-over-func loop is not counted", `package lower
func Each(yield func(int) bool) { _ = yield(1) }`, `package upper
import "lower"
var sum int
func Plant() { for n := range lower.Each { sum += n } }`, none},
		{"a call within a package or down the import direction is not counted", `package lower
func Leaf() {}`, `package upper
import "lower"
func local() {}
func Plant() { g := local; g(); f := lower.Leaf; f() }`, none},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls, err := reversedCalls(plantedPackages(t,
				plantedPackage{"lower", tc.lower}, plantedPackage{"upper", tc.upper}))
			require.NoError(t, err)
			assert.Equal(t, tc.want, callCounts(calls))
		})
	}
}

// TestCallShape_LeavesOutImportsThroughOthersAndTheRunner plants the calls the
// count leaves out that lower and upper cannot show: one into a package the
// caller imports through another, and one the runner makes.
func TestCallShape_LeavesOutImportsThroughOthersAndTheRunner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pkgs []plantedPackage
	}{
		{"a call into a package imported through another", []plantedPackage{
			{"base", `package base
func Leaf() {}`},
			{"mid", `package mid
import "base"
var Handle = base.Leaf`},
			{"top", `package top
import "mid"
func Plant() { mid.Handle() }`},
		}},
		{"a call the runner makes", []plantedPackage{
			{callRunner, `package barrier
func Catch(f func()) { f() }`},
			{"top", `package top
import "barrier"
func Plant() { barrier.Catch(func() {}) }`},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls, err := reversedCalls(plantedPackages(t, tc.pkgs...))
			require.NoError(t, err)
			assert.Empty(t, callCounts(calls))
		})
	}
}
