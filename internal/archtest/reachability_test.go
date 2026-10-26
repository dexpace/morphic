package archtest_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/dexpace/morphic/ir/irtest"
)

// The callers apiRoots names, one reason per kind of caller.
const (
	compilerAPI = "the OpenAPI compiler's Go API: a caller building Options starts from the default " +
		"rather than transcribing it, and the CLI sets options as text"
	embedderAPI  = "the engine's API for a program driving it from Go, which the CLI does not need"
	transformAPI = "how a transform pass edits a document (docs/architecture.md); no transform ships yet"
	relationAPI  = "a relation ir-design §4.3 defines for the IR's consumers, which no shipped one reads yet"
	factAPI      = "a fact ir-design §4.9 derives for emitters, which are not built yet"
	stringerAPI  = "fmt.Stringer, naming a Kind wherever one is printed; declared in a test file, it would " +
		"make a test print what production does not"
)

// apiRoots names each function no binary of the module reaches that is there
// for another caller, with that caller. The reachability check starts from
// these as well as from every binary's main, so what they call needs no entry
// of its own. A function only tests reach is deleted or moved into its tests;
// an entry here says why one is not.
var apiRoots = map[string]string{
	"compilers/openapi.DefaultStreamingMediaTypes":      compilerAPI,
	"compilers/openapi.DefaultExtensionPromotions":      compilerAPI,
	"compilers/openapi/internal/annotation.Kind.String": stringerAPI,
	"engine.WithCompilers":                              embedderAPI,
	"engine.WithPasses":                                 embedderAPI,
	"engine.Engine.RunSources":                          embedderAPI,
	"engine.Engine.RunDocument":                         embedderAPI,
	"ir.NewEditor":                                      transformAPI,
	"ir.Editor.Type":                                    transformAPI,
	"ir.Editor.UpdateType":                              transformAPI,
	"ir.Editor.ReplaceType":                             transformAPI,
	"ir.Editor.RemoveType":                              transformAPI,
	"ir.Editor.Finish":                                  transformAPI,
	"ir.Supertypes":                                     relationAPI,
	"ir.ExposedProps":                                   relationAPI,
	"ir/irfacts.Index.Subtypes":                         factAPI,
	"ir/irfacts.Index.Usage":                            factAPI,
	"ir/irfacts.Index.Recursive":                        factAPI,
	"ir/irfacts.Index.Component":                        factAPI,
}

// testSupport names the packages the reachability check skips, each with the
// reason: their production files exist for tests, so only tests reaching them
// is what they are for. An entry ending in subtreeSuffix names a subtree.
var testSupport = map[string]string{
	"internal" + subtreeSuffix: "test and tooling infrastructure outside the pipeline (CLAUDE.md), " +
		"whose binaries reach only part of it",
	"ir/irtest":                              "the golden and fixture helpers the IR's tests share",
	"compilers/openapi/internal/openapitest": "the scaffolding the compiler's test packages share",
	"compilers/openapi/internal/ynode":       "the YAML node builders the compiler's internal tests share",
}

// TestReachability_OnlyTestsReachNoFunction holds every function the module's
// packages declare to a caller outside its tests: a binary's main, or an entry
// of apiRoots. Each entry is held the other way too, failing once a binary
// reaches it. Reachability is rapid type analysis over the whole program, which
// follows calls through interfaces to the types the program converts to them,
// so it counts every exported method of such a type as reached: reflection
// could call it.
func TestReachability_OnlyTestsReachNoFunction(t *testing.T) {
	t.Parallel()
	p, loaded := moduleProgram(t)
	require.NotEmpty(t, p.mains, "found no binary, so nothing is reached")
	require.NotEmpty(t, p.funcs, "found no function, so this proves nothing")
	for _, problem := range unreached(p, apiRoots) {
		t.Error(problem)
	}
	for _, entry := range slices.Sorted(maps.Keys(testSupport)) {
		assert.True(t, slices.ContainsFunc(loaded, func(rel string) bool { return supports(entry, rel) }),
			"testSupport names %s, which matches no package", entry)
	}
}

// program is what the reachability check reads: every function the packages
// it holds declare, with a body, and the functions each binary starts from.
type program struct {
	fset  *token.FileSet
	funcs []sourceFunc
	mains []*ssa.Function
}

// sourceFunc is one declared function: its name as apiRoots keys it, the
// package's path below the module and then nodeName's key, and its SSA form.
type sourceFunc struct {
	name string
	fn   *ssa.Function
}

// moduleProgram builds the SSA form of every package of the module, without
// its tests, and returns what the check reads of it, with the paths below the
// module of every package it loaded, testSupport's included.
func moduleProgram(t *testing.T) (program, []string) {
	t.Helper()
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Dir: irtest.RepoRoot(t)}
	loaded, err := packages.Load(cfg, "./...")
	require.NoError(t, err)
	require.Zero(t, packages.PrintErrors(loaded), "the module does not type-check")
	prog, pkgs := ssautil.AllPackages(loaded, ssa.InstantiateGenerics)
	prog.Build()

	p := program{fset: prog.Fset}
	for _, m := range ssautil.MainPackages(pkgs) {
		p.mains = append(p.mains, m.Func("init"), m.Func("main"))
	}
	rels := make([]string, 0, len(loaded))
	for _, pkg := range loaded {
		rel := strings.TrimPrefix(pkg.PkgPath, module+"/")
		rels = append(rels, rel)
		if !supported(rel) {
			p.funcs = append(p.funcs, declaredFuncs(prog, rel, pkg.Syntax, pkg.TypesInfo)...)
		}
	}
	return p, rels
}

// supported reports whether an entry of testSupport names the package at rel.
func supported(rel string) bool {
	for entry := range testSupport {
		if supports(entry, rel) {
			return true
		}
	}
	return false
}

// supports reports whether the testSupport entry names the package at rel.
func supports(entry, rel string) bool {
	root, subtree := strings.CutSuffix(entry, subtreeSuffix)
	return rel == root || subtree && strings.HasPrefix(rel, root+"/")
}

// declaredFuncs returns the functions files declare with a body, named under
// rel. A sealed sum's marker method is left out: it is never called, and its
// interface, which a type assertion reads, is what it is for.
func declaredFuncs(prog *ssa.Program, rel string, files []*ast.File, info *types.Info) []sourceFunc {
	var out []sourceFunc
	for _, fd := range funcDecls(files) {
		obj, isFunc := info.Defs[fd.Name].(*types.Func)
		if !isFunc || marker(fd, obj) {
			continue
		}
		out = append(out, sourceFunc{name: rel + "." + nodeName(obj), fn: prog.FuncValue(obj)})
	}
	return out
}

// marker reports whether fd, declaring fn, is a marker method: unexported,
// empty, taking and returning nothing, and part of an interface its package
// declares and its receiver implements.
func marker(fd *ast.FuncDecl, fn *types.Func) bool {
	if fd.Recv == nil || fd.Name.IsExported() || len(fd.Body.List) > 0 ||
		fd.Type.Params.NumFields() > 0 || fd.Type.Results.NumFields() > 0 {
		return false
	}
	recv, scope := fn.Signature().Recv().Type(), fn.Pkg().Scope()
	return slices.ContainsFunc(scope.Names(), func(name string) bool {
		tn, isType := scope.Lookup(name).(*types.TypeName)
		if !isType {
			return false
		}
		iface, isIface := tn.Type().Underlying().(*types.Interface)
		if !isIface || !types.Implements(recv, iface) {
			return false
		}
		_, index, _ := types.LookupFieldOrMethod(iface, false, fn.Pkg(), fn.Name())
		return index != nil
	})
}

// unreached returns what the check finds in p: each function neither a binary
// nor an entry of roots reaches, each entry a binary reaches, and each entry
// naming no function of p.
func unreached(p program, roots map[string]string) []string {
	byName := make(map[string]*ssa.Function, len(p.funcs))
	for _, f := range p.funcs {
		byName[f.name] = f.fn
	}
	var problems []string
	starts := slices.Clone(p.mains)
	for _, name := range slices.Sorted(maps.Keys(roots)) {
		fn, ok := byName[name]
		if !ok {
			problems = append(problems, unknownRoot(name))
			continue
		}
		starts = append(starts, fn)
	}
	fromMains, fromAll := reachedFrom(p.fset, p.mains), reachedFrom(p.fset, starts)
	for _, f := range p.funcs {
		at := p.fset.Position(f.fn.Pos())
		_, isRoot := roots[f.name]
		switch {
		case isRoot && fromMains[at]:
			problems = append(problems, reachedRoot(f.name))
		case !fromAll[at]:
			problems = append(problems, onlyTests(f.name))
		}
	}
	return problems
}

// reachedFrom returns where each function roots reach is declared. A position
// stands for the function, so an instance of a generic one counts for it.
func reachedFrom(fset *token.FileSet, roots []*ssa.Function) map[token.Position]bool {
	out := map[token.Position]bool{}
	for fn := range rta.Analyze(roots, false).Reachable {
		if fn.Pos().IsValid() {
			out[fset.Position(fn.Pos())] = true
		}
	}
	return out
}

// onlyTests, reachedRoot and unknownRoot spell the three problems unreached
// reports.
func onlyTests(name string) string {
	return name + ": only tests reach it; delete it, move it into its tests, or name its caller in apiRoots"
}

func reachedRoot(name string) string {
	return name + ": a binary reaches it, so its apiRoots entry goes"
}

func unknownRoot(name string) string { return name + ": apiRoots names no function" }

// plantedProgram builds src, a main package importing nothing, as the check
// reads a program.
func plantedProgram(t *testing.T, src string) program {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "planted.go", src, 0)
	require.NoError(t, err)
	files := []*ast.File{f}
	pkg, info, err := ssautil.BuildPackage(&types.Config{}, fset, types.NewPackage("planted", "main"), files,
		ssa.InstantiateGenerics)
	require.NoError(t, err)
	return program{fset: fset, funcs: declaredFuncs(pkg.Prog, "planted", files, info),
		mains: []*ssa.Function{pkg.Func("init"), pkg.Func("main")}}
}

// TestReachability_ReportsWhatNoRootReaches plants programs whose functions a
// main reaches, or does not, in each way the check reads, and roots that hold
// or go stale.
func TestReachability_ReportsWhatNoRootReaches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		src   string
		roots map[string]string
		want  []string
	}{
		{"a function main calls is reached and one it does not is not", `package main
func main() { used() }
func used() {}
func unused() {}`, nil, []string{onlyTests("planted.unused")}},
		{"a call through an interface reaches the method a converted type declares", `package main
type I interface{ m() int }
type T struct{}
func (T) m() int { return 0 }
func (T) n() int { return 1 }
func main() { var i I = T{}; _ = i.m() }`, nil, []string{onlyTests("planted.T.n")}},
		{"an exported method of a converted type is reached, as reflection could call it", `package main
type T struct{}
func (T) M() {}
func main() { var v any = T{}; _ = v }`, nil, nil},
		{"a root reaches what it calls", `package main
func main() {}
func root() { callee() }
func callee() {}`, map[string]string{"planted.root": "planted"}, nil},
		{"a root main reaches is stale", `package main
func main() { root() }
func root() {}`, map[string]string{"planted.root": "planted"}, []string{reachedRoot("planted.root")}},
		{"a root naming nothing is refused", `package main
func main() {}`, map[string]string{"planted.gone": "planted"}, []string{unknownRoot("planted.gone")}},
		{"a marker method is left out", `package main
type S interface{ sealed() }
type T struct{}
func (T) sealed() {}
func main() {}`, nil, nil},
		{"an empty method no interface names is held as any other", `package main
type T struct{}
func (T) spare() {}
func main() {}`, nil, []string{onlyTests("planted.T.spare")}},
		{"a generic function counts once any instance is reached", `package main
func id[T any](v T) T { return v }
func spare[T any](v T) T { return v }
func main() { _ = id(1) }`, nil, []string{onlyTests("planted.spare")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, unreached(plantedProgram(t, tc.src), tc.roots))
		})
	}
}
