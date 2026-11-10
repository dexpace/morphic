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

// callShapeBaseline counts the calls each production package, by its path
// below the module, makes into each other one it does not import, directly or
// through others: calls only a function, an interface or a type argument handed
// to the caller can make, which no import rule sees.
// TestCallShape_HeldToTheBaseline fails when a count rises, and when one falls
// until it is lowered here (GitHub #1478). A call of a kind an entry keeps, such
// as a pass's walk callback, raises its count the way an allowlist entry is
// added. Each entry says what its calls are.
var callShapeBaseline = map[string]map[string]int{
	"compilers": {
		// The registry calls the methods of each compiler it holds, DecodeOptions
		// among them as it resolves a compiler's options: a designed seam.
		"compilers/openapi": 6,
	},
	"compilers/compile": {
		// The settlement queue runs what a lowering deferred, once the registry is
		// whole and in order (#898), and Types.Promise's queue builds a node a
		// reference names that no lowering built: designed seams. Types.Intern
		// also calls the build schema hands it before it returns: an immediate
		// callback.
		"compilers/openapi/internal/operation": 2,
		"compilers/openapi/internal/schema":    4,
	},
	"compilers/openapi/internal/bind": {
		// bind.Config's seven functions, which #1488 keeps under #1477's rule: I/O
		// (Open), policy bind may not import (Locate, Refuse), a live query
		// (Written), lookups through load's key normalization (Through, Beside),
		// and a dialect's rows, built only once a schema names it (Dialect).
		"compilers/openapi/internal/load": 7,
	},
	"compilers/openapi/internal/load": {
		// DocumentReader, through which load reads the documents references and
		// overlays name: I/O at a designed seam.
		"engine":        2,
		"engine/reader": 6,
	},
	"compilers/openapi/internal/lowering": {
		// Promoter.Referent and Remembered each call the function they are handed
		// before they return: immediate callbacks.
		"compilers/openapi/internal/schema": 9,
	},
	"compilers/openapi/internal/merge": {
		// What a fold asks the walk that declared the field, through merge.Walk:
		// Mint twice, Alike twice and Interned once. merge.Source, a lazy render of
		// a declaration nobody may keep. And the predicate SelectBase calls before
		// it returns, an immediate callback.
		"compilers/openapi/internal/schema": 7,
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
		// oas.Options' Locate, Seen and Owned, which #1488 keeps under #1477's
		// rule: the checker places its own findings, and an Owned's, to drop the
		// repeat an alias makes, a policy, and Seen observes the walk, as Owned
		// takes each value it leaves to the load phase, traversal callbacks.
		"compilers/openapi/internal/load": 12,
	},
	"compilers/openapi/internal/provenance": {
		// provenance.Tree's Pointers, a node's pointer in the tree it places, read
		// off an index built on the first node asked about, under the context of
		// the load reading the tree: a lazily built index.
		"compilers/openapi/internal/load": 1,
		"compilers/openapi/internal/scan": 1,
	},
	"compilers/openapi/internal/resolve": {
		// resolve.Scope's Holds, Beside and Writes, which #1488 keeps under #1477's
		// rule: a lazily built index (Holds), and queries of load's index of the
		// fields written beside a path item $ref (Beside, Writes).
		"compilers/openapi/internal/load": 3,
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
		"ir/irfacts":      3,
		"ir/irverify":     2,
		"pass":            13,
	},
}

// callShapeRatchet is how a difference from callShapeBaseline reads.
var callShapeRatchet = ratchet{
	name:   "callShapeBaseline",
	counts: callShapeBaseline,
	what:   "calls into",
	rise: "each reaches a package the caller's does not import, which no import rule sees; hand the " +
		"caller a value instead, or, for a call of a kind the table keeps, raise the count, giving a new " +
		"pair an entry that says what its calls are (GitHub #1488)",
}

// TestCallShape_HeldToTheBaseline holds the calls each production package makes
// into packages it does not import to callShapeBaseline. A run that analyzed
// nothing finds no call, and fails each count the table holds. A count does not
// say which calls make it up, so a call removed and another added in one pair
// leave it as it was. Nor does it see a call back that runs through a library,
// as through a function sync.OnceFunc wraps: only the edges between the
// module's own functions are read, as a library's own interface calls reach
// every type implementing them.
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
		caller, callee := declaredFunc(e.Caller.Func), declaredFunc(e.Callee.Func)
		if e.Site == nil || caller.Pkg == nil || callee.Pkg == nil {
			return fmt.Errorf("the call graph holds a call from %s to %s that no source makes", e.Caller.Func, e.Callee.Func)
		}
		from, ownCaller := p.own[caller.Pkg]
		to, ownCallee := p.own[callee.Pkg]
		reversed := ownCaller && ownCallee && caller.Pkg != callee.Pkg && !imports[caller.Pkg][callee.Pkg.Pkg]
		if !reversed || from == callRunner || protocolCall(e.Site) || rangeBody(callee) {
			return nil
		}
		k := key{callPos(e.Site), callee}
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
	sortCalls(out)
	return out, err
}

// callPos returns where site is in the source. The call a range-over-func loop
// makes of its iterator has no position of its own, so it is placed where the
// body it hands the iterator is: at the loop.
func callPos(site ssa.CallInstruction) token.Pos {
	if pos := site.Pos(); pos.IsValid() {
		return pos
	}
	if args := site.Common().Args; len(args) == 1 {
		if body, isClosure := args[0].(*ssa.MakeClosure); isClosure {
			if fn, isFunc := body.Fn.(*ssa.Function); isFunc && rangeBody(fn) {
				return fn.Pos()
			}
		}
	}
	return token.NoPos
}

// sortCalls sorts each pair's calls by where they are, then by what they reach,
// as the call graph's map yields its edges in no order.
func sortCalls(calls map[string]map[string][]reversedCall) {
	for _, byCallee := range calls {
		for _, at := range byCallee {
			slices.SortFunc(at, func(a, b reversedCall) int {
				return cmp.Or(cmp.Compare(a.at.Filename, b.at.Filename), cmp.Compare(a.at.Offset, b.at.Offset),
					cmp.Compare(a.callee, b.callee))
			})
		}
	}
}

// callGraph returns the call graph of what p's packages can run: variable type
// analysis over the functions rapid type analysis reaches from each function
// they declare and each package's initializer, seeded with the graph rapid type
// analysis builds. ssautil.AllFunctions is no basis: it answers differently from
// call to call on one program, and a wrapper it leaves out takes its calls with
// it. Neither a generic function, which rapid type analysis cannot start from,
// nor one of a package testSupport names, which may serve tests alone, is a
// root: each is reached from its callers, if it has any.
func callGraph(p program) *callgraph.Graph {
	roots := make([]*ssa.Function, 0, len(p.funcs)+len(p.own))
	for _, f := range p.funcs {
		if _, forTests := testSupport[f.pkg]; !forTests && f.fn.TypeParams().Len() == 0 {
			roots = append(roots, f.fn)
		}
	}
	for _, pkg := range slices.SortedFunc(maps.Keys(p.own), func(a, b *ssa.Package) int {
		return cmp.Compare(a.Pkg.Path(), b.Pkg.Path())
	}) {
		roots = append(roots, pkg.Func("init"))
	}
	reached := rta.Analyze(roots, true /* buildCallGraph */)
	funcs := make(map[*ssa.Function]bool, len(reached.Reachable))
	for fn := range reached.Reachable {
		funcs[fn] = true
	}
	// VTA keeps one node for each struct field and for the elements of each
	// slice, map or channel type, so a call of what one holds is credited with
	// all that any reached function stores in one: a count can hold a call no
	// run makes (TestCallShape_CreditsEachCallWithAllItsTypeHolds).
	graph := vta.CallGraph(funcs, reached.CallGraph)
	graph.DeleteSyntheticNodes()
	return graph
}

// callRunner is the package whose calls are not counted: barrier runs the code
// its caller hands it, so each call it makes into another package runs back up.
// So a function the caller was handed and passes on to barrier as it is goes
// unseen, as a call back through a library does
// (TestCallShape_MissesAFunctionHandedOnToTheRunner).
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

// declaredFunc returns the function the source declares for fn: the generic
// function of an instance, an anonymous one inside an instance included, and fn
// itself otherwise. Its Pkg is the package declaring it, which an anonymous
// function shares with the one enclosing it and a synthetic wrapper lacks.
func declaredFunc(fn *ssa.Function) *ssa.Function {
	if origin := fn.Origin(); origin != nil {
		return origin
	}
	return fn
}

// protocolCall reports whether site calls, through any interface, a method
// named as one of the standard library's protocols names it, which a type
// anywhere may implement: Error, String, Format, GoString and Unwrap.
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
		p.own[prog.CreatePackage(checked, files, info, true /* importable */)] = src.path
		funcs, err := declaredFuncs(prog, src.path, files, info)
		require.NoError(t, err)
		p.funcs = append(p.funcs, funcs...)
	}
	prog.Build()
	return p
}

// lowerAndUpper returns the two packages most plants are written as, lower and
// upper, which imports it, each source given without its package clause or
// upper's import.
func lowerAndUpper(lower, upper string) []plantedPackage {
	return []plantedPackage{
		{"lower", "package lower\n" + lower},
		{"upper", "package upper\nimport \"lower\"\n" + upper},
	}
}

// plantedCounts returns how many calls reversedCalls finds for each pair of
// pkgs, planted.
func plantedCounts(t *testing.T, pkgs []plantedPackage) map[string]map[string]int {
	t.Helper()
	calls, err := reversedCalls(plantedPackages(t, pkgs...))
	require.NoError(t, err)
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
	assert.Equal(t, map[string]map[string]int{"lower": {"upper": 1}}, plantedCounts(t, lowerAndUpper(
		`type Index struct{ later func() }
func New(later func()) *Index { return &Index{later: later} }
func (x *Index) Settle() { x.later() }`,
		`var settled int
func Plant() { lower.New(func() { settled++ }).Settle() }`)))
}

// TestCallShape_CountsWhatIsHandedDownAndCalled plants ways upper can hand lower
// a function, an interface or a type argument whose method lower then calls,
// each of which counts once.
func TestCallShape_CountsWhatIsHandedDownAndCalled(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, lower, upper string }{
		{"a method value stored and called later", `var held func() int
func Hold(f func() int) { held = f }
func Run() int { return held() }`, `type T struct{}
func (T) n() int { return 1 }
func Plant() { lower.Hold(T{}.n); _ = lower.Run() }`},
		{"an interface", `type Visitor interface{ Visit() }
func Walk(v Visitor) { v.Visit() }`, `type v struct{}
func (v) Visit() {}
func Plant() { lower.Walk(v{}) }`},
		{"a method promoted from an embedded type, through its wrapper", `type Visitor interface{ Visit() }
func Walk(v Visitor) { v.Visit() }`, `type inner struct{}
func (inner) Visit() {}
type outer struct{ inner }
func Plant() { lower.Walk(&outer{}) }`},
		{"a String method value: only an interface call of one is a protocol call", `var held func() string
func Hold(f func() string) { held = f }
func Run() string { return held() }`, `type T struct{}
func (T) String() string { return "" }
func Plant() { lower.Hold(T{}.String); _ = lower.Run() }`},
		{"a method of a type argument", `type Visitor interface{ Visit() }
func Walk[V Visitor](v V) { v.Visit() }`, `type v struct{}
func (*v) Visit() {}
func Plant() { lower.Walk(&v{}) }`},
		{"a String method of a type argument: it is called on its type, through no interface", `type Stringer interface{ String() string }
func Show[S Stringer](s S) string { return s.String() }`, `type v struct{}
func (v) String() string { return "" }
func Plant() string { return lower.Show(v{}) }`},
		{"a deferred call", `func Run(f func()) { defer f() }`, `func Plant() { lower.Run(func() {}) }`},
		{"a goroutine", `func Run(f func()) { go f() }`, `func Plant() { lower.Run(func() {}) }`},
		{"a function sent over a channel", `var queue = make(chan func(), 1)
func Send(f func()) { queue <- f }
func Run() { (<-queue)() }`, `func Plant() { lower.Send(func() {}); lower.Run() }`},
		{"a function held in a map", `var held = map[string]func(){}
func Hold(f func()) { held["f"] = f }
func Run() { held["f"]() }`, `func Plant() { lower.Hold(func() {}); lower.Run() }`},
		{"a function a package initializer hands down", `var held func()
func Hold(f func()) bool { held = f; return true }
func Run() { held() }`, `var _ = lower.Hold(func() {})`},
		{"an iterator a range-over-func loop runs: its call has no position of its own",
			`func Sum(seq func(func(int) bool)) (n int) { for x := range seq { n += x }; return n }`,
			`func each(yield func(int) bool) { _ = yield(1) }
func Plant() int { return lower.Sum(each) }`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := plantedCounts(t, lowerAndUpper(tc.lower, tc.upper))
			assert.Equal(t, map[string]map[string]int{"lower": {"upper": 1}}, got)
		})
	}
}

// TestCallShape_CountsEachSiteAndFunctionOnce plants one site reaching several
// functions, and sites and functions that instances of a generic function
// share: a call is a site and a function it reaches, whatever the instances.
func TestCallShape_CountsEachSiteAndFunctionOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, lower, upper string
		want               int
	}{
		{"one site reaching two functions counts each", `func Run(f func()) { f() }`, `func a() {}
func b() {}
func Plant() { lower.Run(a); lower.Run(b) }`, 2},
		{"a site in a generic function counts once for the function its instances reach",
			`func Apply[T any](v T, f func()) { f() }`, `func done() {}
func Plant() { lower.Apply(1, done); lower.Apply("", done) }`, 1},
		{"a generic function reached through two instances counts once", `func Run(f func()) { f() }`,
			`func each[T any]() {}
func Plant() { lower.Run(each[int]); lower.Run(each[string]) }`, 1},
		{"a site in a closure of a generic function counts", `func Apply[T any](f func()) { func() { f() }() }`,
			`func done() {}
func Plant() { lower.Apply[int](done) }`, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := plantedCounts(t, lowerAndUpper(tc.lower, tc.upper))
			assert.Equal(t, map[string]map[string]int{"lower": {"upper": tc.want}}, got)
		})
	}
}

// TestCallShape_MissesACallBackThroughALibrary pins what the count does not
// see: upper hands lower a function that lower has a library wrap and calls
// later, and another that lower hands a library to call, so each call back
// into upper is made by the library.
func TestCallShape_MissesACallBackThroughALibrary(t *testing.T) {
	t.Parallel()
	p := plantedPackages(t,
		plantedPackage{"lib", `package lib
func Once(f func()) func() { done := false; return func() { if !done { done = true; f() } } }
func Each(xs []int, f func(int)) { for _, x := range xs { f(x) } }`},
		plantedPackage{"lower", `package lower
import "lib"
var held func()
func Hold(f func()) { held = lib.Once(f) }
func Run() { held() }
func Sweep(f func(int)) { lib.Each([]int{1}, f) }`},
		plantedPackage{"upper", `package upper
import "lower"
func Plant() { lower.Hold(func() {}); lower.Run(); lower.Sweep(func(int) {}) }`})
	for pkg, name := range p.own {
		if name == "lib" {
			delete(p.own, pkg) // a library, as the standard library is to the module
		}
	}
	calls, err := reversedCalls(p)
	require.NoError(t, err)
	assert.Empty(t, calls)
}

// TestCallShape_MissesAFunctionHandedOnToTheRunner pins what leaving the
// runner's calls out costs: lower hands barrier the function upper handed it,
// so the call into upper is barrier's, and goes unseen. Called inside a closure
// of lower's own, it is lower's call, and counts.
func TestCallShape_MissesAFunctionHandedOnToTheRunner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, run string
		want      map[string]map[string]int
	}{
		{"handed on as it is", "barrier.Catch(f)", map[string]map[string]int{}},
		{"called in a closure of lower's", "barrier.Catch(func() { f() })",
			map[string]map[string]int{"lower": {"upper": 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, plantedCounts(t, []plantedPackage{
				{callRunner, "package barrier\nfunc Catch(f func()) { f() }"},
				{"lower", "package lower\nimport \"barrier\"\nfunc Run(f func()) { " + tc.run + " }"},
				{"upper", "package upper\nimport \"lower\"\nfunc Plant() { lower.Run(func() {}) }"},
			}))
		})
	}
}

// TestCallShape_CreditsEachCallWithAllItsTypeHolds pins where the count reaches
// too far: a and b each hand a function, through a struct field, a slice, a map
// or a channel, to a function of lower's that only it calls, yet each of
// lower's two sites is credited with both functions. Handed as a parameter,
// each function is credited to the one site it reaches.
func TestCallShape_CreditsEachCallWithAllItsTypeHolds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, held, call, hand string
		want                   int
	}{
		{"a struct field", "c struct{ F func() }", "c.F()", "struct{ F func() }{F: func() {}}", 2},
		{"a slice", "fs []func()", "fs[0]()", "[]func(){func() {}}", 2},
		{"a map", "m map[string]func()", `m["x"]()`, `map[string]func(){"x": func() {}}`, 2},
		{"a channel", "c chan func()", "(<-c)()",
			"func() chan func() { c := make(chan func(), 1); c <- func() {}; return c }()", 2},
		{"a parameter", "f func()", "f()", "func() {}", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lower := fmt.Sprintf("package lower\nfunc RunA(%[1]s) { %[2]s }\nfunc RunB(%[1]s) { %[2]s }", tc.held, tc.call)
			got := plantedCounts(t, []plantedPackage{
				{"lower", lower},
				{"a", "package a\nimport \"lower\"\nfunc Plant() { lower.RunA(" + tc.hand + ") }"},
				{"b", "package b\nimport \"lower\"\nfunc Plant() { lower.RunB(" + tc.hand + ") }"},
			})
			assert.Equal(t, map[string]map[string]int{"lower": {"a": tc.want, "b": tc.want}}, got)
		})
	}
}

// TestCallShape_LeavesOutWhatOnlyTestsReach plants the shape that a test-support
// package's variadic walk takes: no binary calls it, but the analysis reads one
// []Visitor as any other, so it seems to call each visitor upper hands lower's
// walk. Named as a package testSupport lists, it is no root and counts nothing;
// named otherwise, it counts, so the plant reaches the analysis.
func TestCallShape_LeavesOutWhatOnlyTestsReach(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, testSupport, "the plant must be named as a test-support package")
	support := slices.Sorted(maps.Keys(testSupport))[0]
	tests := []struct {
		name, path string
		want       map[string]map[string]int
	}{
		{"a test-support package", support, map[string]map[string]int{"lower": {"upper": 1}}},
		{"any other package", "measure", map[string]map[string]int{"lower": {"upper": 1}, "measure": {"upper": 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pkgs := append(lowerAndUpper(`type Visitor interface{ Visit() }
func Walk(vs ...Visitor) { for _, v := range vs { v.Visit() } }`, `type v struct{}
func (v) Visit() {}
func Plant() { lower.Walk(v{}) }`), plantedPackage{tc.path, `package measure
import "lower"
func Visits(vs ...lower.Visitor) { for _, v := range vs { v.Visit() } }`})
			assert.Equal(t, tc.want, plantedCounts(t, pkgs))
		})
	}
}

// TestCallShape_LeavesOutWhatDoesNotRunBack plants each call the count leaves
// out, where the call graph holds it: a protocol call, a range-over-func body,
// calls within a package and down the import direction, a call into a package
// the caller imports through another, and a call the runner makes.
func TestCallShape_LeavesOutWhatDoesNotRunBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pkgs []plantedPackage
	}{
		{"a protocol method called through an interface", lowerAndUpper(
			`func Describe(err error) string { return err.Error() }`, `type failure struct{}
func (failure) Error() string { return "" }
func Plant() string { return lower.Describe(failure{}) }`)},
		{"the body of a range-over-func loop", lowerAndUpper(`func Each(yield func(int) bool) { _ = yield(1) }`,
			`var sum int
func Plant() { for n := range lower.Each { sum += n } }`)},
		{"a call within a package or down the import direction", lowerAndUpper(`func Leaf() {}`,
			`func local() {}
func Plant() { g := local; g(); f := lower.Leaf; f() }`)},
		{"a call into a package imported through another", []plantedPackage{
			{"base", "package base\nfunc Leaf() {}"},
			{"mid", "package mid\nimport \"base\"\nvar Handle = base.Leaf"},
			{"top", "package top\nimport \"mid\"\nfunc Plant() { mid.Handle() }"},
		}},
		{"a call the runner makes", []plantedPackage{
			{callRunner, "package barrier\nfunc Catch(f func()) { f() }"},
			{"top", "package top\nimport \"barrier\"\nfunc Plant() { barrier.Catch(func() {}) }"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, plantedCounts(t, tc.pkgs))
		})
	}
}
