package archtest_test

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/dexpace/morphic/internal/decls"
)

// libraryModule is the parser library whose reference resolution the compiler
// is moving off (GitHub #558): its resolver overflows the stack on a
// reference-only cycle, and its answer depends on declaration order.
const libraryModule = "github.com/speakeasy-api/openapi"

// The two anchors the library's resolution API is derived from: its resolver,
// and the method that resolves a schema, whose package holds the state that
// resolving writes. Everything else is read off the library's sources.
const (
	resolverPackage = libraryModule + "/references"
	resolverName    = "Resolve"
	schemaPackage   = libraryModule + "/jsonschema/oas3"
	schemaType      = "JSONSchema"
	schemaResolve   = "Resolve"
)

// resolutionBaseline is how many times each production package, keyed by its
// repo-relative path, uses each member of the library's resolution API.
// TestResolutionCalls_HeldToTheBaseline fails when a count rises, since each new
// use deepens the dependence on the library's resolution, and when one falls
// until the count here is lowered to match, so the map only shrinks (GitHub
// #920). A call through an interface both kinds of reference satisfy counts
// toward each, so its schema count reaches zero only once the interface is
// split.
var resolutionBaseline = map[string]map[string]int{
	"compilers/openapi/internal/annotation": {
		"oas3.JSONSchema.GetReferenceResolutionInfo": 1,
	},
	"compilers/openapi/internal/defs": {
		"oas3.Schema.GetEffectiveBaseURI": 1,
		"oas3.Schema.GetSchemaRegistry":   1,
	},
	"compilers/openapi/internal/load": {
		"oas3.JSONSchema.GetReferenceResolutionInfo": 2,
		"oas3.JSONSchema.GetResolvedSchema":          1,
		"oas3.JSONSchema.IsResolved":                 3,
		"oas3.JSONSchema.Resolve":                    4,
		"oas3.Schema.GetOwningDocument":              1,
		"openapi.Reference.Resolve":                  2,
	},
	"compilers/openapi/internal/operation": {
		"oas3.JSONSchema.GetResolvedSchema": 1,
	},
	"compilers/openapi/internal/resolve": {
		"oas3.JSONSchema.GetReferenceResolutionInfo": 2,
		"oas3.JSONSchema.GetResolvedSchema":          1,
	},
	"compilers/openapi/internal/schema": {
		"oas3.JSONSchema.GetResolvedSchema": 2,
	},
}

// reviewedMembers is the resolution API as derived from the library version
// go.mod requires, by short name. A member nothing calls shows in no baseline
// count, so a bump that adds one or drops one fails
// TestResolutionAPI_IsTheReviewedSet until it is looked at and this list is
// updated: a member dropped silently would let a later call into it go
// uncounted.
var reviewedMembers = []string{
	"oas3.Inline",
	"oas3.JSONSchema.GetAbsRef",
	"oas3.JSONSchema.GetDocumentBaseURI",
	"oas3.JSONSchema.GetImmediateReference",
	"oas3.JSONSchema.GetParent",
	"oas3.JSONSchema.GetReferenceChain",
	"oas3.JSONSchema.GetReferenceResolutionInfo",
	"oas3.JSONSchema.GetResolvedObject",
	"oas3.JSONSchema.GetResolvedSchema",
	"oas3.JSONSchema.GetSchemaRegistry",
	"oas3.JSONSchema.GetTopLevelParent",
	"oas3.JSONSchema.GetTopLevelReference",
	"oas3.JSONSchema.IsResolved",
	"oas3.JSONSchema.MustGetResolvedSchema",
	"oas3.JSONSchema.Resolve",
	"oas3.JSONSchema.SetDocumentBaseURI",
	"oas3.JSONSchema.SetParent",
	"oas3.JSONSchema.SetSchemaRegistry",
	"oas3.JSONSchema.SetTopLevelParent",
	"oas3.JSONSchema.ShallowCopy",
	"oas3.NewReferencedScheme",
	"oas3.Schema.GetEffectiveBaseURI",
	"oas3.Schema.GetOwningDocument",
	"oas3.Schema.GetSchemaRegistry",
	"oas3.Schema.PopulateWithContext",
	"oas3.Schema.SetEffectiveBaseURI",
	"oas3.Schema.SetOwningDocument",
	"oas3.Schema.ShallowCopy",
	"oas3.SchemaRegistryImpl.GetBaseURI",
	"oas3.SchemaRegistryImpl.RegisterSchema",
	"openapi.BuildIndex",
	"openapi.Bundle",
	"openapi.Encoding.GetContentType",
	"openapi.Inline",
	"openapi.Localize",
	"openapi.OpenAPI.ResolveAllReferences",
	"openapi.Optimize",
	"openapi.Reference.Resolve",
	"openapi.Upgrade",
	"references.Resolve",
}

// libFunc is one function the library declares with a body, as the type checker
// reads it: what it names, and the struct fields it touches and writes, each
// keyed by fieldKey. A call through an interface names no declaration, so it
// records no edge.
type libFunc struct {
	fn      *types.Func
	calls   map[string]bool
	touches map[string]bool
	writes  map[string]bool
}

// libGraph holds every libFunc of a set of packages, keyed by
// types.Func.FullName, which names a generic function's declaration however a
// call instantiates it.
type libGraph map[string]*libFunc

// addFuncs adds the functions files declare to g.
func (g libGraph) addFuncs(files []*ast.File, info *types.Info) {
	for _, fd := range funcDecls(files) {
		fn, ok := info.Defs[fd.Name].(*types.Func)
		if !ok {
			continue
		}
		f := &libFunc{fn: fn, calls: map[string]bool{}, touches: map[string]bool{}, writes: map[string]bool{}}
		for n := range ast.Preorder(fd.Body) {
			f.read(n, info)
		}
		g[fn.FullName()] = f
	}
}

// read records what one node of f's body names, touches or writes.
func (f *libFunc) read(n ast.Node, info *types.Info) {
	switch n := n.(type) {
	case *ast.Ident:
		if callee, ok := info.Uses[n].(*types.Func); ok {
			f.calls[callee.Origin().FullName()] = true
		}
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[n]; ok && sel.Kind() == types.FieldVal {
			f.touches[fieldKey(sel)] = true
		}
	case *ast.AssignStmt:
		for _, lhs := range n.Lhs {
			if key, ok := writtenField(lhs, info); ok {
				f.writes[key] = true
			}
		}
	case *ast.IncDecStmt:
		if key, ok := writtenField(n.X, info); ok {
			f.writes[key] = true
		}
	}
}

// writtenField returns the field an assignment to lhs writes: the one it names,
// or the one whose element, pointee or slice it indexes.
func writtenField(lhs ast.Expr, info *types.Info) (string, bool) {
	for {
		switch e := ast.Unparen(lhs).(type) {
		case *ast.IndexExpr:
			lhs = e.X
		case *ast.StarExpr:
			lhs = e.X
		case *ast.SelectorExpr:
			sel, ok := info.Selections[e]
			if !ok || sel.Kind() != types.FieldVal {
				return "", false
			}
			return fieldKey(sel), true
		default:
			return "", false
		}
	}
}

// fieldKey names a field by the type that declares it, followed through any
// embedding a promoted selection crosses: "path.Type.field".
func fieldKey(sel *types.Selection) string {
	owner := sel.Recv()
	path := sel.Index()
	for _, i := range path[:len(path)-1] {
		st, ok := derefNamed(owner).Underlying().(*types.Struct)
		if !ok {
			break
		}
		owner = st.Field(i).Type()
	}
	return typeKey(owner) + "." + sel.Obj().Name()
}

// derefNamed strips aliases and one pointer from t.
func derefNamed(t types.Type) types.Type {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	return t
}

// typeKey names a type "path.Name", generic instantiations by their origin.
func typeKey(t types.Type) string {
	named, ok := derefNamed(t).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return types.TypeString(t, nil)
	}
	obj := named.Origin().Obj()
	return obj.Pkg().Path() + "." + obj.Name()
}

// resolutionAPI derives the library's resolution API from g: every exported
// function or method that, through calls the type checker resolves, runs the
// resolver, or touches a field of the schema's package that resolving a schema
// writes. A non-schema reference's answer is read by GetObject, the accessor of
// every parameter and response, and stays with the library (GitHub #920), so
// only running its resolver counts; Walk reads no resolution state, and
// GetTarget and Validate reach it only through GetObject. Each member maps to
// why it is one.
func resolutionAPI(g libGraph, resolver, schemaResolve, schemaPkg string) map[string]string {
	state := writtenFrom(g, schemaResolve, schemaPkg+".")
	why := map[string]string{}
	var queue []string
	for _, name := range slices.Sorted(maps.Keys(g)) {
		if reason := rootReason(name, g[name], resolver, state); reason != "" {
			why[name] = reason
			queue = append(queue, name)
		}
	}

	callers := map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(g)) {
		for _, callee := range slices.Sorted(maps.Keys(g[name].calls)) {
			callers[callee] = append(callers[callee], name)
		}
	}
	for len(queue) > 0 {
		callee := queue[0]
		queue = queue[1:]
		for _, caller := range callers[callee] {
			if _, seen := why[caller]; !seen {
				why[caller] = "calls " + callee
				queue = append(queue, caller)
			}
		}
	}

	maps.DeleteFunc(why, func(name, _ string) bool { return !exportedAPI(g[name].fn) })
	return why
}

// writtenFrom returns the fields under prefix that from, or anything it calls,
// writes: the state resolving a schema leaves behind.
func writtenFrom(g libGraph, from, prefix string) map[string]bool {
	written := map[string]bool{}
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		f, ok := g[queue[0]]
		queue = queue[1:]
		if !ok {
			continue
		}
		for field := range f.writes {
			if strings.HasPrefix(field, prefix) {
				written[field] = true
			}
		}
		for callee := range f.calls {
			if !seen[callee] {
				seen[callee] = true
				queue = append(queue, callee)
			}
		}
	}
	return written
}

// rootReason says why f belongs to the API on its own account, or "" when it
// does not: it is the resolver, calls it, or touches resolution state.
func rootReason(name string, f *libFunc, resolver string, state map[string]bool) string {
	switch {
	case name == resolver:
		return "is the resolver"
	case f.calls[resolver]:
		return "runs the resolver"
	}
	for _, field := range slices.Sorted(maps.Keys(f.touches)) {
		if state[field] {
			return "touches " + field
		}
	}
	return ""
}

// exportedAPI reports whether fn can be called from outside its package: it is
// exported, and so is the type it is a method of.
func exportedAPI(fn *types.Func) bool {
	if !fn.Exported() {
		return false
	}
	recv := fn.Signature().Recv()
	if recv == nil {
		return true
	}
	named, ok := derefNamed(recv.Type()).(*types.Named)
	return ok && named.Obj().Exported()
}

// resolutionMember is one member of the API: the function or method, and the
// short name the baseline gives it, "pkg.Type.Method" or "pkg.Func".
type resolutionMember struct {
	full, pkg, recv, name, short string
}

// membersOf describes the API's members, refusing two that would share a short
// name, since the baseline could not tell them apart.
func membersOf(g libGraph, api map[string]string) ([]resolutionMember, error) {
	out := make([]resolutionMember, 0, len(api))
	shorts := map[string]string{}
	for _, full := range slices.Sorted(maps.Keys(api)) {
		fn := g[full].fn
		m := resolutionMember{full: full, pkg: fn.Pkg().Path(), name: fn.Name(), short: fn.Pkg().Name() + "."}
		if recv := fn.Signature().Recv(); recv != nil {
			named, ok := derefNamed(recv.Type()).(*types.Named)
			if !ok {
				return nil, fmt.Errorf("%s has a receiver that is no named type", full)
			}
			m.recv = named.Obj().Name()
			m.short += m.recv + "."
		}
		m.short += m.name
		if other, taken := shorts[m.short]; taken {
			return nil, fmt.Errorf("%s and %s share the short name %s", full, other, m.short)
		}
		shorts[m.short] = full
		out = append(out, m)
	}
	return out, nil
}

// libraryAPI derives the library's resolution API once per test binary, from
// the packages of it the module's production code reaches.
var libraryAPI = sync.OnceValues(deriveResolutionAPI)

// deriveResolutionAPI reads the library's sources and derives the API from its
// two anchors.
func deriveResolutionAPI() ([]resolutionMember, error) {
	paths, err := libraryPackages()
	if err != nil {
		return nil, err
	}
	pkgs, err := decls.LoadEach(paths...)
	if err != nil {
		return nil, err
	}

	g := libGraph{}
	var resolver, schema string
	for _, p := range pkgs {
		g.addFuncs(p.Syntax, p.Info)
		switch p.Types.Path() {
		case resolverPackage:
			resolver, err = anchor(p, "", resolverName)
		case schemaPackage:
			schema, err = anchor(p, schemaType, schemaResolve)
		}
		if err != nil {
			return nil, err
		}
	}
	if resolver == "" || schema == "" {
		return nil, fmt.Errorf("%s and %s are not both among the library's packages", resolverPackage, schemaPackage)
	}

	api := resolutionAPI(g, resolver, schema, schemaPackage)
	if _, ok := api[schema]; !ok {
		return nil, fmt.Errorf("%s is not in the API derived from it", schema)
	}
	return membersOf(g, api)
}

// anchor returns the full name of the function name p declares, or of the
// method name of the type typeName when that is not "".
func anchor(p *decls.Package, typeName, name string) (string, error) {
	if typeName == "" {
		fn, ok := p.Types.Scope().Lookup(name).(*types.Func)
		if !ok {
			return "", fmt.Errorf("%s declares no function %s; re-derive the anchor", p.Types.Path(), name)
		}
		return fn.FullName(), nil
	}
	named, err := p.Named(typeName)
	if err != nil {
		return "", err
	}
	for m := range named.Methods() {
		if m.Name() == name {
			return m.FullName(), nil
		}
	}
	return "", fmt.Errorf("%s.%s has no method %s; re-derive the anchor", p.Types.Path(), typeName, name)
}

// libraryPackages lists the library's packages the module's production code
// imports, directly or through one another: the sources the API is read from.
func libraryPackages() ([]string, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps,
		Env:  append(os.Environ(), "GOPACKAGESDRIVER=off"),
	}
	roots, err := packages.Load(cfg, module+"/...")
	if err != nil {
		return nil, err
	}
	var paths []string
	packages.Visit(roots, nil, func(p *packages.Package) {
		if strings.HasPrefix(p.PkgPath, libraryModule+"/") {
			paths = append(paths, p.PkgPath)
		}
	})
	if len(paths) == 0 {
		return nil, fmt.Errorf("no production package imports %s", libraryModule)
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

// resolutionUses returns where p uses each member, by short name. A use is any
// identifier the type checker resolves to a member: a call, a method value or
// expression, a method promoted through an embedded field. One resolved to an
// interface's method, a constraint's included, counts toward each member of that
// name whose type has every method the interface declares, since the call may
// reach it. A method reached by reflection names no identifier and is not seen.
func resolutionUses(p *decls.Package, members []resolutionMember) map[string][]token.Position {
	byFull := map[string]resolutionMember{}
	byName := map[string][]resolutionMember{}
	for _, m := range members {
		byFull[m.full] = m
		byName[m.name] = append(byName[m.name], m)
	}

	uses := map[string][]token.Position{}
	for id, obj := range p.Info.Uses {
		fn, ok := obj.(*types.Func)
		if !ok {
			continue
		}
		reached := dispatchedTo(p, fn, byName[fn.Name()])
		if m, ok := byFull[fn.Origin().FullName()]; ok {
			reached = []resolutionMember{m}
		}
		for _, m := range reached {
			uses[m.short] = append(uses[m.short], p.Fset.Position(id.Pos()))
		}
	}
	for _, at := range uses {
		slices.SortFunc(at, func(a, b token.Position) int {
			return cmp.Or(cmp.Compare(a.Filename, b.Filename), cmp.Compare(a.Offset, b.Offset))
		})
	}
	return uses
}

// dispatchedTo returns the candidates a call through fn may reach, when fn is an
// interface's method: each one whose type, in p's view of it, has every method
// the interface declares.
func dispatchedTo(p *decls.Package, fn *types.Func, candidates []resolutionMember) []resolutionMember {
	recv := fn.Signature().Recv()
	if recv == nil || len(candidates) == 0 {
		return nil
	}
	iface, ok := recv.Type().Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	var out []resolutionMember
	for _, m := range candidates {
		if m.recv != "" && satisfies(p, m, iface) {
			out = append(out, m)
		}
	}
	return out
}

// satisfies reports whether m's type, through a pointer, has a method of every
// name iface declares.
func satisfies(p *decls.Package, m resolutionMember, iface *types.Interface) bool {
	pkg, ok := p.Imported(m.pkg)
	if !ok {
		return false
	}
	tn, ok := pkg.Scope().Lookup(m.recv).(*types.TypeName)
	if !ok {
		return false
	}
	methods := types.NewMethodSet(types.NewPointer(tn.Type()))
	for want := range iface.Methods() {
		if methods.Lookup(want.Pkg(), want.Name()) == nil {
			return false
		}
	}
	return true
}

// baselineFindings compares uses, by package and member, with baseline and
// returns one message per difference, sorted: a rise names each use, a fall
// asks for the baseline to be lowered.
func baselineFindings(baseline map[string]map[string]int, uses map[string]map[string][]token.Position, root string) []string {
	var out []string
	for _, pkg := range sortedUnion(uses, baseline) {
		for _, m := range sortedUnion(uses[pkg], baseline[pkg]) {
			got, want := len(uses[pkg][m]), baseline[pkg][m]
			switch {
			case got > want:
				out = append(out, fmt.Sprintf("%s: %d uses of %s, over its baseline of %d; each deepens the "+
					"compiler's dependence on the library's reference resolution (GitHub #558): %s",
					pkg, got, m, want, positions(uses[pkg][m], root)))
			case got < want:
				out = append(out, fmt.Sprintf("%s: %d uses of %s, under its baseline of %d; lower "+
					"resolutionBaseline to match", pkg, got, m, want))
			}
		}
	}
	return out
}

// sortedUnion returns the keys of a and b, once each and sorted.
func sortedUnion[A, B any](a map[string]A, b map[string]B) []string {
	keys := slices.Collect(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// positions renders where each use is, as "file:line", the file relative to
// root when it is under it.
func positions(at []token.Position, root string) string {
	parts := make([]string, len(at))
	for i, pos := range at {
		file := pos.Filename
		if rel, err := filepath.Rel(root, file); err == nil && filepath.IsLocal(rel) {
			file = filepath.ToSlash(rel)
		}
		parts[i] = fmt.Sprintf("%s:%d", file, pos.Line)
	}
	return strings.Join(parts, ", ")
}

// productionLoad type-checks every production package of the module once per
// test binary, for the sweeps that read them all.
var productionLoad = sync.OnceValues(func() ([]*decls.Package, error) {
	paths, err := productionPaths()
	if err != nil {
		return nil, err
	}
	return decls.LoadEach(paths...)
})

// productionPaths lists the module's packages that hold a production Go file,
// one the build leaves out included, so that decls.LoadEach refuses it rather
// than the sweep passing over it.
func productionPaths() ([]string, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	listed, err := packages.Load(cfg, module+"/...")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range listed {
		files := slices.Concat(p.GoFiles, p.IgnoredFiles)
		if slices.ContainsFunc(files, func(f string) bool { return isProductionGoFile(filepath.Base(f)) }) {
			paths = append(paths, p.PkgPath)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s/... lists no production package", module)
	}
	return paths, nil
}

// TestResolutionCalls_HeldToTheBaseline holds every production package's uses
// of the library's resolution API to resolutionBaseline. The API is derived
// from the library's sources rather than listed, so a member a bump adds, or
// one a list would have missed, is counted from the start.
func TestResolutionCalls_HeldToTheBaseline(t *testing.T) {
	t.Parallel()
	members, err := libraryAPI()
	require.NoError(t, err)
	pkgs, err := productionLoad()
	require.NoError(t, err)

	uses := map[string]map[string][]token.Position{}
	swept := false
	for _, p := range pkgs {
		rel := strings.TrimPrefix(p.Types.Path(), module+"/")
		swept = swept || rel == "compilers/openapi/internal/load"
		if u := resolutionUses(p, members); len(u) > 0 {
			uses[rel] = u
		}
	}
	require.True(t, swept, "the sweep missed the package that drives the resolver, so a clean result proves nothing")
	for _, finding := range baselineFindings(resolutionBaseline, uses, repoRoot(t)) {
		t.Error(finding)
	}
}

// TestResolutionAPI_IsTheReviewedSet holds the API derived from the real
// library to reviewedMembers, in both directions.
func TestResolutionAPI_IsTheReviewedSet(t *testing.T) {
	t.Parallel()
	members, err := libraryAPI()
	require.NoError(t, err)
	shorts := make([]string, len(members))
	for i, m := range members {
		shorts[i] = m.short
	}
	slices.Sort(shorts)
	assert.Equal(t, reviewedMembers, shorts,
		"the library's resolution API changed: review each member added or dropped, then update reviewedMembers")
}

// plantedLibrary is a library in miniature: Resolve is its resolver and
// Schema.Resolve resolves a schema, writing cache directly and parent through a
// call. Each function's comment says whether the API holds it, and why.
const plantedLibrary = `package p

func Resolve() {} // the resolver

type Schema struct {
	cache  *int
	parent *Schema
	seen   map[string]bool
	box    *int
	count  int
	name   string
	other  int
}

func (s *Schema) Resolve()     { s.cache = new(int); s.count++; s.link(); Resolve() } // runs the resolver
func (s *Schema) link()        { s.parent = s; s.seen["s"] = true; *s.box = 1 }      // unexported
func (s *Schema) Cached() bool { return s.cache != nil }                            // touches cache
func (s *Schema) Up() *Schema  { return s.parent }                                  // touches parent, written through link
func (s *Schema) Seen() bool   { return s.seen["s"] }                               // touches seen, written through an index
func (s *Schema) Box() int     { return *s.box }                                    // touches box, written through its pointer
func (s *Schema) Count() int   { return s.count }                                   // touches count, incremented
func (s *Schema) Name() string { return s.name }                                    // not: Resolve never writes name
func (s *Schema) SetOther()    { s.other++ }                                        // not: written, but not by Resolve
func (s *Schema) cached() bool { return s.cache != nil }                            // unexported
func Through(s *Schema) bool   { return s.cached() }                                // reaches cache through cached
func Runs()                    { helper() }                                         // reaches the resolver through helper
func helper()                  { Resolve() }                                        // unexported
func Plain() int               { return 1 }                                         // not: reaches neither

type hidden struct{ s *Schema }

func (h hidden) Cached() bool { return h.s.cache != nil } // not: its type is unexported

type Doc struct{ Schema }

func (d *Doc) Promoted() bool { return d.cache != nil } // touches cache, promoted from Schema
`

// TestResolutionAPI_DerivesThePlantedLibrarysMembers derives the API of
// plantedLibrary and requires exactly the members its comments name, each for
// its reason: a list of names could not see a member the library adds.
func TestResolutionAPI_DerivesThePlantedLibrarysMembers(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "planted.go", plantedLibrary, 0)
	require.NoError(t, err)
	info := &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	_, err = (&types.Config{}).Check("p", fset, []*ast.File{f}, info)
	require.NoError(t, err)
	g := libGraph{}
	g.addFuncs([]*ast.File{f}, info)

	assert.Equal(t, map[string]string{
		"p.Resolve":           "is the resolver",
		"(*p.Schema).Resolve": "runs the resolver",
		"(*p.Schema).Cached":  "touches p.Schema.cache",
		"(*p.Schema).Up":      "touches p.Schema.parent",
		"(*p.Schema).Seen":    "touches p.Schema.seen",
		"(*p.Schema).Box":     "touches p.Schema.box",
		"(*p.Schema).Count":   "touches p.Schema.count",
		"p.Through":           "calls (*p.Schema).cached",
		"p.Runs":              "calls p.helper",
		"(*p.Doc).Promoted":   "touches p.Schema.cache",
	}, resolutionAPI(g, "p.Resolve", "(*p.Schema).Resolve", "p"))
}

// plantedUses uses the API in every spelling a use takes, each line counted
// marked with the member it counts toward, beside look-alikes that are not uses:
// another type's method of a member's name, a library method that is no member,
// and an interface only a non-schema reference satisfies.
const plantedUses = `package diag

import (
	"context"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
)

type schemaRef = oas3.JSONSchema[oas3.Referenceable]

type embeds struct{ *schemaRef }

type resolved interface {
	IsReference() bool
	GetResolvedSchema() *oas3.JSONSchema[oas3.Concrete]
}

type objectRef interface {
	IsResolved() bool
	GetObject() *soa.PathItem
}

type lookalike struct{}

func (lookalike) GetResolvedSchema() bool { return false }

func viaConstraint[S interface {
	IsResolved() bool
	GetReference() references.Reference
}](s S) bool {
	return s.IsResolved() // counts oas3.JSONSchema.IsResolved
}

func plantedResolution(ctx context.Context, s *schemaRef, doc *soa.OpenAPI, item objectRef, r resolved) {
	_ = s.GetResolvedSchema() // counts oas3.JSONSchema.GetResolvedSchema
	read := s.GetResolvedSchema // counts oas3.JSONSchema.GetResolvedSchema
	_ = read
	_ = (*schemaRef).GetResolvedSchema // counts oas3.JSONSchema.GetResolvedSchema
	_ = embeds{s}.GetResolvedSchema() // counts oas3.JSONSchema.GetResolvedSchema
	_ = r.GetResolvedSchema() // counts oas3.JSONSchema.GetResolvedSchema
	_ = viaConstraint(s)
	_, _ = doc.ResolveAllReferences(ctx, soa.ResolveAllOptions{}) // counts openapi.OpenAPI.ResolveAllReferences
	_ = lookalike{}.GetResolvedSchema()
	_ = s.GetSchema()
	_ = item.IsResolved()
}
`

// TestResolutionUses_PlantedSpellingsAreCounted plants plantedUses into a
// package that uses none of the API and requires exactly the uses its markers
// name. Without it, a counter that recognized nothing would pass the pin.
func TestResolutionUses_PlantedSpellingsAreCounted(t *testing.T) {
	t.Parallel()
	members, err := libraryAPI()
	require.NoError(t, err)
	p := loadOverlaid(t, "compilers/openapi/internal/diag", "zz_planted_resolution.go", plantedUses)

	const marker = "// counts "
	var want []string
	for i, line := range strings.Split(plantedUses, "\n") {
		if _, member, ok := strings.Cut(line, marker); ok {
			want = append(want, fmt.Sprintf("%d %s", i+1, member))
		}
	}
	var got []string
	for member, at := range resolutionUses(p, members) {
		for _, pos := range at {
			assert.Equal(t, "zz_planted_resolution.go", filepath.Base(pos.Filename))
			got = append(got, fmt.Sprintf("%d %s", pos.Line, member))
		}
	}
	require.NotEmpty(t, want)
	assert.ElementsMatch(t, want, got)
}

// TestBaselineFindings_EveryDifferenceFails holds the comparison to its two
// directions: a rise, in a package the baseline lists or one it does not, names
// each use, and a fall, to zero included, asks for the baseline to be lowered.
func TestBaselineFindings_EveryDifferenceFails(t *testing.T) {
	t.Parallel()
	root := filepath.FromSlash("/repo")
	at := func(lines ...int) []token.Position {
		out := make([]token.Position, len(lines))
		for i, l := range lines {
			out[i] = token.Position{Filename: filepath.Join(root, "x", "x.go"), Line: l}
		}
		return out
	}
	baseline := map[string]map[string]int{"held": {"lib.A": 2, "lib.B": 1}, "gone": {"lib.A": 1}}

	assert.Empty(t, baselineFindings(baseline, map[string]map[string][]token.Position{
		"held": {"lib.A": at(1, 2), "lib.B": at(3)}, "gone": {"lib.A": at(4)},
	}, root), "uses that match the baseline")

	assert.Equal(t, []string{
		"gone: 0 uses of lib.A, under its baseline of 1; lower resolutionBaseline to match",
		"held: 3 uses of lib.A, over its baseline of 2; each deepens the compiler's dependence on the " +
			"library's reference resolution (GitHub #558): x/x.go:1, x/x.go:2, x/x.go:5",
		"held: 0 uses of lib.B, under its baseline of 1; lower resolutionBaseline to match",
		"new: 1 uses of lib.C, over its baseline of 0; each deepens the compiler's dependence on the " +
			"library's reference resolution (GitHub #558): x/x.go:7",
	}, baselineFindings(baseline, map[string]map[string][]token.Position{
		"held": {"lib.A": at(1, 2, 5)}, "new": {"lib.C": at(7)},
	}, root))
}
