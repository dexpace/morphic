package archtest_test

import (
	"fmt"
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
	"engine.Engine.RunSources":                          embedderAPI,
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

// testSupport names each package whose production files exist for tests, with
// what they serve, so the reachability check spares what only tests reach in
// it. An entry sparing nothing fails, so the list cannot outlive its reasons.
var testSupport = map[string]string{
	"ir/irtest":                              "the golden and fixture helpers the IR's tests share",
	"compilers/openapi/internal/openapitest": "the scaffolding the compiler's test packages share",
	"compilers/openapi/internal/ynode":       "the YAML node builders the compiler's internal tests share",
	"internal/decls": "the type-checked reads of declarations the tests pinning hand-written lists " +
		"share; the census binary reads one load of it",
	"internal/harness": "the annotation-retention vocabulary each format's retention test shares, " +
		"which leaves the rest of the package unread until the vocabulary moves out (#1123)",
	"internal/jsonshape": "the IR's JSON shape, which IRVersion's pin test describes and the census counts",
	"internal/keydoc":    "the one reader of the keyed documents, for the tests holding each to the code",
	"internal/leakcheck": "the goroutine leak check each test binary's TestMain runs",
}

// TestReachability_OnlyTestsReachNoFunction holds every function the module's
// packages declare to a caller outside its tests: a binary's main, or an entry
// of apiRoots. What the packages testSupport names declare is spared. Both lists
// are held the other way too: an apiRoots entry a binary reaches fails, and so
// does a testSupport entry sparing nothing. Reachability is rapid type analysis
// over the whole program: it follows calls through interfaces to the types the
// program converts to them, and counts every exported method of such a type as
// reached, since reflection could call it.
func TestReachability_OnlyTestsReachNoFunction(t *testing.T) {
	t.Parallel()
	p := moduleProgram(t)
	require.NotEmpty(t, p.mains, "found no binary, so nothing is reached")
	require.NotEmpty(t, p.funcs, "found no function, so this proves nothing")
	for _, problem := range unreached(p, apiRoots, testSupport) {
		t.Error(problem)
	}
}

// program is what the reachability check reads: every function the module's
// packages declare with a body, and the functions each binary starts from.
type program struct {
	fset  *token.FileSet
	funcs []sourceFunc
	mains []*ssa.Function
}

// sourceFunc is one declared function: the path of its package below the
// module, its name as apiRoots keys it (that path, a dot and nodeName's key),
// and its SSA form.
type sourceFunc struct {
	pkg  string
	name string
	fn   *ssa.Function
}

// moduleProgram builds the SSA form of every package of the module and of
// what it imports, without tests, and returns what the check reads of it.
func moduleProgram(t *testing.T) program {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps |
			packages.NeedModule | packages.NeedTypesSizes,
		Dir: irtest.RepoRoot(t),
	}
	roots, err := packages.Load(cfg, "./...")
	require.NoError(t, err)
	require.Zero(t, packages.PrintErrors(roots), "the module does not load")

	prog := ssa.NewProgram(token.NewFileSet(), ssa.InstantiateGenerics)
	built := map[*packages.Package]builtPackage{}
	packages.Visit(roots, nil, func(pkg *packages.Package) { built[pkg] = buildPackage(t, prog, pkg, built) })
	prog.Build()

	p := program{fset: prog.Fset}
	rootPkgs := make([]*ssa.Package, 0, len(roots))
	for _, root := range roots {
		b := built[root]
		rootPkgs = append(rootPkgs, b.ssa)
		rel := strings.TrimPrefix(root.PkgPath, module+"/")
		p.funcs = append(p.funcs, declaredFuncs(t, prog, rel, b.files, b.info)...)
	}
	for _, m := range ssautil.MainPackages(rootPkgs) {
		p.mains = append(p.mains, m.Func("init"), m.Func("main"))
	}
	return p
}

// builtPackage is one package as buildPackage leaves it: its syntax, what the
// type checker recorded of it, and its SSA form, still to be built.
type builtPackage struct {
	files []*ast.File
	info  *types.Info
	ssa   *ssa.Package
}

// buildPackage parses and type-checks pkg against the packages it imports,
// which built already holds, at the language version its module declares, as
// the go command does, and adds it to prog. Packages are checked one at a time,
// not by go/packages: it checks independent packages at once, and two go/types
// checkers sharing an import can race (#855).
func buildPackage(t *testing.T, prog *ssa.Program, pkg *packages.Package,
	built map[*packages.Package]builtPackage,
) builtPackage {
	t.Helper()
	if pkg.PkgPath == "unsafe" {
		return builtPackage{ssa: prog.CreatePackage(types.Unsafe, nil, nil, true)}
	}
	files := make([]*ast.File, 0, len(pkg.CompiledGoFiles))
	for _, name := range pkg.CompiledGoFiles {
		f, err := parser.ParseFile(prog.Fset, name, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		files = append(files, f)
	}
	info := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	conf := types.Config{Sizes: pkg.TypesSizes, Importer: importerFunc(func(path string) (*types.Package, error) {
		imported, ok := built[pkg.Imports[path]]
		if !ok {
			return nil, fmt.Errorf("%s is not built before %s, which imports it", path, pkg.PkgPath)
		}
		return imported.ssa.Pkg, nil
	})}
	if pkg.Module != nil && pkg.Module.GoVersion != "" {
		conf.GoVersion = "go" + pkg.Module.GoVersion
	}
	checked, err := conf.Check(pkg.PkgPath, prog.Fset, files, info)
	require.NoError(t, err, "type-check %s", pkg.PkgPath)
	return builtPackage{files: files, info: info, ssa: prog.CreatePackage(checked, files, info, true)}
}

// importerFunc is a types.Importer written as a function.
type importerFunc func(path string) (*types.Package, error)

// Import returns the package path names.
func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// declaredFuncs returns the functions files declare with a body, named under
// rel. A sealed sum's marker method is left out: it is never called, and its
// interface, which a type assertion reads, is what it is for.
func declaredFuncs(t *testing.T, prog *ssa.Program, rel string, files []*ast.File, info *types.Info) []sourceFunc {
	t.Helper()
	var out []sourceFunc
	for _, fd := range funcDecls(files) {
		obj, isFunc := info.Defs[fd.Name].(*types.Func)
		require.True(t, isFunc, "%s.%s is not a function to the type checker", rel, fd.Name.Name)
		if marker(fd, obj) {
			continue
		}
		fn := prog.FuncValue(obj)
		require.NotNil(t, fn, "%s.%s has no SSA form", rel, fd.Name.Name)
		out = append(out, sourceFunc{pkg: rel, name: rel + "." + nodeName(obj), fn: fn})
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
// nor an entry of roots reaches, outside the packages spared names; each entry
// of roots a binary reaches, or naming no function of p; and each entry of
// spared that spares no function.
func unreached(p program, roots, spared map[string]string) []string {
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
	sparing := map[string]bool{}
	for _, f := range p.funcs {
		at := p.fset.Position(f.fn.Pos())
		_, isRoot := roots[f.name]
		_, isSpared := spared[f.pkg]
		switch {
		case isRoot && fromMains[at]:
			problems = append(problems, reachedRoot(f.name))
		case fromAll[at]:
		case isSpared:
			sparing[f.pkg] = true
		default:
			problems = append(problems, onlyTests(f.name))
		}
	}
	for _, pkg := range slices.Sorted(maps.Keys(spared)) {
		if !sparing[pkg] {
			problems = append(problems, sparesNothing(pkg))
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

// onlyTests, reachedRoot, unknownRoot and sparesNothing spell the problems
// unreached reports.
func onlyTests(name string) string {
	return name + ": only tests reach it; delete it, move it into its tests, or name its caller in apiRoots"
}

func reachedRoot(name string) string {
	return name + ": a binary reaches it, so its apiRoots entry goes"
}

func unknownRoot(name string) string {
	return name + ": the module declares no such function, so its apiRoots entry goes"
}

func sparesNothing(pkg string) string {
	return pkg + ": no function only tests reach is declared there, so its testSupport entry goes"
}

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
	return program{fset: fset, funcs: declaredFuncs(t, pkg.Prog, "planted", files, info),
		mains: []*ssa.Function{pkg.Func("init"), pkg.Func("main")}}
}

// TestReachability_ReportsWhatNoRootReaches plants programs whose functions a
// main reaches, or does not, in each way the check reads, roots that hold or go
// stale, and testSupport entries that spare a function or go.
func TestReachability_ReportsWhatNoRootReaches(t *testing.T) {
	t.Parallel()
	spared := map[string]string{"planted": "planted"}
	tests := []struct {
		name   string
		src    string
		roots  map[string]string
		spared map[string]string
		want   []string
	}{
		{"a function main calls is reached and one it does not is not", `package main
func main() { used() }
func used() {}
func unused() {}`, nil, nil, []string{onlyTests("planted.unused")}},
		{"a call through an interface reaches the method a converted type declares", `package main
type I interface{ m() int }
type T struct{}
func (T) m() int { return 0 }
func (T) n() int { return 1 }
func main() { var i I = T{}; _ = i.m() }`, nil, nil, []string{onlyTests("planted.T.n")}},
		{"an exported method of a converted type is reached, as reflection could call it", `package main
type T struct{}
func (T) M() {}
func main() { var v any = T{}; _ = v }`, nil, nil, nil},
		{"a root reaches what it calls", `package main
func main() {}
func root() { callee() }
func callee() {}`, map[string]string{"planted.root": "planted"}, nil, nil},
		{"a root main reaches is stale", `package main
func main() { root() }
func root() {}`, map[string]string{"planted.root": "planted"}, nil, []string{reachedRoot("planted.root")}},
		{"a root naming nothing is refused", `package main
func main() {}`, map[string]string{"planted.gone": "planted"}, nil, []string{unknownRoot("planted.gone")}},
		{"a marker method is left out", `package main
type S interface{ sealed() }
type T struct{}
func (T) sealed() {}
func main() {}`, nil, nil, nil},
		{"an empty method no interface names is held as any other", `package main
type T struct{}
func (T) spare() {}
func main() {}`, nil, nil, []string{onlyTests("planted.T.spare")}},
		{"a generic function counts once any instance is reached", `package main
func id[T any](v T) T { return v }
func spare[T any](v T) T { return v }
func main() { _ = id(1) }`, nil, nil, []string{onlyTests("planted.spare")}},
		{"a package testSupport names is spared what only tests reach", `package main
func main() {}
func spare() {}`, nil, spared, nil},
		{"a testSupport entry sparing nothing goes", `package main
func main() { used() }
func used() {}`, nil, spared, []string{sparesNothing("planted")}},
		{"a testSupport entry naming no package goes", `package main
func main() {}`, nil, map[string]string{"gone": "gone"}, []string{sparesNothing("gone")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, unreached(plantedProgram(t, tc.src), tc.roots, tc.spared))
		})
	}
}
