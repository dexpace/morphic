// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir/irtest"
)

// modelReadBaseline counts, by repo-relative package path, how many times each
// selects a member of the parser library's schema types outside the statement
// adapter (statement's model.go), or names a bridge (bridgeKey). It holds the
// two modelHolders besides the adapter that read the model's fields: the
// operation walk, and reftable/typed, which builds the bindings from the model
// before any statement is read. A count rising is a second reading beside the
// statement, and one falling asks for the map to be lowered, so it only shrinks
// (GitHub #1991).
var modelReadBaseline = map[string]map[string]int{
	"compilers/openapi/internal/reftable/typed": {
		"GetCore":   1,
		"GetSchema": 1,
		"IsBool":    3,
		"Ref":       1,
	},
	"compilers/openapi/internal/operation": {
		"GetAdditionalProperties":  2,
		"GetAllOf":                 2,
		"GetAnyOf":                 4,
		"GetContentEncoding":       2,
		"GetDependentSchemas":      1,
		"GetElse":                  1,
		"GetFormat":                1,
		"GetIf":                    1,
		"GetItems":                 2,
		"GetOneOf":                 4,
		"GetPatternProperties":     2,
		"GetPrefixItems":           1,
		"GetProperties":            3,
		"GetRootNode":              1,
		"GetSchema":                4,
		"GetThen":                  1,
		"GetType":                  6,
		"GetUnevaluatedProperties": 2,
		"IsSchema":                 2,
		"statement.Parsed":         12,
	},
}

// modelReadRatchet is how a difference from modelReadBaseline reads.
var modelReadRatchet = ratchet{
	name:   "modelReadBaseline",
	counts: modelReadBaseline,
	what:   "reads of",
	rise: "each is a second reading of the source beside compilers/openapi/internal/statement, " +
		"which reads the parser's schema once (GitHub #1991)",
}

// modelHolders names every production package that holds a parser schema
// type, keyed by its repo-relative path, with why. The import allowlist cannot
// hold one out, since an expression has the type with nothing imported when it
// is inferred from a call, so a package holds one only when listed here.
// TestModelHolders_NoOtherPackageHoldsAParserSchema fails for a package that
// holds one and is not listed, and for a listed package that holds none, so
// an entry cannot outlive its reason.
var modelHolders = map[string]string{
	"compilers/openapi": "ranges the document's component schemas to name them",
	"compilers/openapi/internal/reftable/typed": "builds the bindings from the model before " +
		"any statement is read",
	"compilers/openapi/internal/load": "builds the bindings from the model before " +
		"any statement is read",
	"compilers/openapi/internal/openapitest": "test scaffolding that builds the model a compile reads",
	"compilers/openapi/internal/operation":   "the operation walk, whose reads modelReadBaseline counts",
	"compilers/openapi/internal/service":     "ranges the document's component schemas to name them",
	"compilers/openapi/internal/statement":   "the adapter, and the handles it keeps",
}

// bridgeKey is the key modelReadBaseline counts each use of the statement's
// bridges under: a function handing back the parser's value a statement was
// read from, which the operation walk reads the model through as surely as
// by a selection on it.
const bridgeKey = "statement.Parsed"

// statementPackage is the package the bridges are declared in, and bridges
// names them.
const statementPackage = module + "/compilers/openapi/internal/statement"

var bridges = []string{"Parsed", "ParsedAt"}

// sortPositions orders at by file, then by offset within it.
func sortPositions(at []token.Position) {
	slices.SortFunc(at, func(a, b token.Position) int {
		if c := strings.Compare(a.Filename, b.Filename); c != 0 {
			return c
		}
		return a.Offset - b.Offset
	})
}

// bridgeUses returns where loadedPackage names one of the statement's bridges.
func bridgeUses(loadedPackage *decls.Package) []token.Position {
	var out []token.Position
	for id, obj := range loadedPackage.Info.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != statementPackage || !slices.Contains(bridges, fn.Name()) {
			continue
		}
		out = append(out, loadedPackage.Fset.Position(id.Pos()))
	}
	sortPositions(out)
	return out
}

// schemaModel names the parser library's schema types: a package and the
// names it declares them by, a generic one standing for each of its
// instances.
type schemaModel struct {
	pkg   string
	names []string
}

// parserSchema is the library's schema object and the schema-or-boolean
// wrapping it.
var parserSchema = schemaModel{pkg: schemaPackage, names: []string{"Schema", "JSONSchema"}}

// holds reports whether t, its pointers removed, is one of m's types or an
// instance of one.
func (m schemaModel) holds(t types.Type) bool {
	named, ok := pointee(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Origin().Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == m.pkg && slices.Contains(m.names, obj.Name())
}

// mentions reports whether t is one of m's types or is built from one, through
// pointers, slices, arrays, maps, channels, signatures, anonymous structs and
// interfaces, and the type arguments of an instance, so an expression whose
// type is inferred from a call is found with no import of m's package. It does
// not look inside a defined type, whose declaration is where its own
// expressions are found, and it stops there, which bounds the recursion by the
// nesting of one written type.
func (m schemaModel) mentions(t types.Type) bool {
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		if m.holds(t) {
			return true
		}
		for arg := range t.TypeArgs().Types() {
			if m.mentions(arg) {
				return true
			}
		}
	case *types.Pointer:
		return m.mentions(t.Elem())
	case *types.Slice:
		return m.mentions(t.Elem())
	case *types.Array:
		return m.mentions(t.Elem())
	case *types.Chan:
		return m.mentions(t.Elem())
	case *types.Map:
		return m.mentions(t.Key()) || m.mentions(t.Elem())
	case *types.Signature:
		return m.mentionsTuple(t.Params()) || m.mentionsTuple(t.Results())
	case *types.Tuple:
		return m.mentionsTuple(t)
	case *types.Struct:
		for f := range t.Fields() {
			if m.mentions(f.Type()) {
				return true
			}
		}
	case *types.Interface:
		for fn := range t.Methods() {
			if m.mentions(fn.Type()) {
				return true
			}
		}
	}
	return false
}

// mentionsTuple reports whether any of tuple's variables is a type m mentions.
func (m schemaModel) mentionsTuple(tuple *types.Tuple) bool {
	for v := range tuple.Variables() {
		if m.mentions(v.Type()) {
			return true
		}
	}
	return false
}

// reaches reports whether sel selects a member of one of m's types: on a value
// of one, or through a field embedding one, which promotes its members.
func (m schemaModel) reaches(sel *types.Selection) bool {
	if m.holds(sel.Recv()) {
		return true
	}
	t, path := sel.Recv(), sel.Index()
	for _, i := range path[:len(path)-1] {
		s, ok := pointee(t).Underlying().(*types.Struct)
		if !ok {
			return false
		}
		t = s.Field(i).Type()
		if m.holds(t) {
			return true
		}
	}
	return false
}

// pointee returns t with its aliases and pointers removed.
func pointee(t types.Type) types.Type {
	t = types.Unalias(t)
	for {
		ptr, ok := t.(*types.Pointer)
		if !ok {
			return t
		}
		t = types.Unalias(ptr.Elem())
	}
}

// modelSelections returns where loadedPackage selects a member of one of
// schemaModelValue's types (reaches), by member, whatever type declares the
// member, so one promoted from an embedded model counts: a field, a method
// value or call, and a method expression. A call through an interface or a type
// parameter and a library function handed a schema select no member of
// schemaModelValue's types, and holds does not recognize a type defined over
// one, such as `type s oas3.Schema`: none of these is counted. It leaves out
// those in the file adapter names and returns how many there were.
func modelSelections(loadedPackage *decls.Package, schemaModelValue schemaModel, adapter string) (outside map[string][]token.Position, inAdapter int) {
	outside = map[string][]token.Position{}
	for expr, sel := range loadedPackage.Info.Selections {
		if !schemaModelValue.reaches(sel) {
			continue
		}
		pos := loadedPackage.Fset.Position(expr.Sel.Pos())
		if pos.Filename == adapter {
			inAdapter++
			continue
		}
		outside[expr.Sel.Name] = append(outside[expr.Sel.Name], pos)
	}
	for _, at := range outside {
		sortPositions(at)
	}
	return outside, inAdapter
}

// TestModelReads_HeldToTheBaseline holds every production package's reads of
// the parser's schema types outside the statement adapter to
// modelReadBaseline. The adapter must exist and read the types itself, so an
// adapter renamed or emptied cannot pass by matching nothing.
func TestModelReads_HeldToTheBaseline(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)
	root := irtest.RepoRoot(t)
	adapter := filepath.Join(root, "compilers", "openapi", "internal", "statement", "model.go")
	_, err = os.Stat(adapter)
	require.NoError(t, err, "the statement adapter is where the baseline's reads move to")

	sites := map[string]map[string][]string{}
	inAdapter := 0
	for _, loadedPackage := range pkgs {
		rel := strings.TrimPrefix(loadedPackage.Types.Path(), module+"/")
		outside, inside := modelSelections(loadedPackage, parserSchema, adapter)
		inAdapter += inside
		if at := bridgeUses(loadedPackage); len(at) > 0 {
			outside[bridgeKey] = at
		}
		for member, at := range outside {
			if sites[rel] == nil {
				sites[rel] = map[string][]string{}
			}
			for _, pos := range at {
				sites[rel][member] = append(sites[rel][member], relPosition(pos, root))
			}
		}
	}
	require.Positive(t, inAdapter, "the adapter reads none of the parser's schema types, so the sweep proves nothing")
	for _, finding := range baselineFindings(modelReadRatchet, sites) {
		t.Error(finding)
	}
}

// plantedModel is a schema library in miniature: a schema type embedding a
// model whose method it promotes, and a generic wrapper of it.
const plantedModel = `package lib

type Model struct{ root int }

func (m *Model) GetRootNode() int { return m.root }

type Schema struct {
	Model
	Type string
}

func (s *Schema) GetType() string { return s.Type }

type JSONSchema[T any] struct{ schema *Schema }

func (j *JSONSchema[T]) GetSchema() *Schema { return j.schema }

type Other struct{ Type string }
`

// plantedAdapter reads the planted model in the adapter's file, where no read
// counts.
const plantedAdapter = `package p

import "lib"

func adapt(js *lib.JSONSchema[int]) string { return js.GetSchema().GetType() }
`

// plantedReads reads the planted model in each spelling a read takes, through
// a struct embedding it too, each line counted marked with the member it
// counts toward, beside look-alikes that are not counted: the embedding
// field itself, a call through an interface, another type's field of a
// member's name, and the embedded model read on its own.
const plantedReads = `package p

import "lib"

type wrapper = lib.JSONSchema[string]

type embeds struct{ *lib.Schema }

func use(js *wrapper, s *lib.Schema, o lib.Other, m *lib.Model, e embeds, g interface{ GetType() string }) {
	_ = js.GetSchema()     // counts GetSchema
	_ = s.GetRootNode()    // counts GetRootNode
	_ = s.Type             // counts Type
	_ = (*lib.Schema).GetType // counts GetType
	read := s.GetType      // counts GetType
	_ = read
	_ = e.Type             // counts Type
	_ = e.GetRootNode()    // counts GetRootNode
	_ = e.Schema
	_ = g.GetType()
	_ = o.Type
	_ = m.GetRootNode()
}
`

// TestModelSelections_PlantedSpellingsAreCounted checks a package beside a
// planted library and requires exactly the reads its markers name outside the
// adapter, and the adapter's own two reads inside it. Without it, a counter
// that recognized nothing, or only methods the schema type declares itself,
// would pass the baseline.
func TestModelSelections_PlantedSpellingsAreCounted(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		f, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		return f
	}
	lib, err := (&types.Config{}).Check("lib", fset, []*ast.File{parse("lib.go", plantedModel)}, nil)
	require.NoError(t, err)
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	imports := importerFunc(func(path string) (*types.Package, error) {
		if path == "lib" {
			return lib, nil
		}
		return importer.Default().Import(path)
	})
	files := []*ast.File{parse("model.go", plantedAdapter), parse("use.go", plantedReads)}
	pkg, err := (&types.Config{Importer: imports}).Check("p", fset, files, info)
	require.NoError(t, err)

	const marker = "// counts "
	var want []string
	for i, line := range strings.Split(plantedReads, "\n") {
		if _, member, ok := strings.Cut(line, marker); ok {
			want = append(want, fmt.Sprintf("%d %s", i+1, member))
		}
	}
	outside, inAdapter := modelSelections(&decls.Package{Types: pkg, Info: info, Syntax: files, Fset: fset},
		schemaModel{pkg: "lib", names: []string{"Schema", "JSONSchema"}}, "model.go")
	var got []string
	for member, at := range outside {
		for _, pos := range at {
			assert.Equal(t, "use.go", pos.Filename)
			got = append(got, fmt.Sprintf("%d %s", pos.Line, member))
		}
	}
	require.NotEmpty(t, want)
	assert.ElementsMatch(t, want, got)
	assert.Equal(t, 2, inAdapter, "the adapter's reads are its own")
}

// holdingExpressions returns where loadedPackage has an expression whose type
// mentions one of schemaModelValue's types, in source order. An expression is a
// name, a call, a selector or a type written out, so one with a type inferred
// from a call is found where it is first used.
func holdingExpressions(loadedPackage *decls.Package, schemaModelValue schemaModel) []token.Position {
	var out []token.Position
	for expr, tv := range loadedPackage.Info.Types {
		if tv.Type != nil && schemaModelValue.mentions(tv.Type) {
			out = append(out, loadedPackage.Fset.Position(expr.Pos()))
		}
	}
	sortPositions(out)
	return out
}

// TestModelHolders_NoOtherPackageHoldsAParserSchema holds every production
// package's expressions of a parser schema type to modelHolders, and every
// holder to holding one. The statement is the schema lowering's one reading of
// the parser's model, so a package that holds the model's value has a second
// one.
func TestModelHolders_NoOtherPackageHoldsAParserSchema(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)
	root := irtest.RepoRoot(t)

	loaded := map[string]bool{}
	for _, loadedPackage := range pkgs {
		rel := strings.TrimPrefix(loadedPackage.Types.Path(), module+"/")
		loaded[rel] = true
		at := holdingExpressions(loadedPackage, parserSchema)
		reason, listed := modelHolders[rel]
		switch {
		case !listed && len(at) > 0:
			t.Errorf("%s holds a parser schema in %d expressions, the first at %s: read the schema from "+
				"compilers/openapi/internal/statement, or list the package in modelHolders with its reason",
				rel, len(at), relPosition(at[0], root))
		case listed && len(at) == 0:
			t.Errorf("%s is listed in modelHolders (%s) and holds no parser schema: remove the entry", rel, reason)
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(modelHolders)) {
		assert.True(t, loaded[rel], "modelHolders names %s, which is no production package", rel)
		assert.NotEmpty(t, modelHolders[rel], "modelHolders lists %s with no reason", rel)
	}
}

// TestModelReads_StatementReadsTheModelInItsAdapterAlone holds the statement
// package's selections on the parser's schema types to model.go, the one file
// that reads the model for it: every other file of the package reads a
// statement.
func TestModelReads_StatementReadsTheModelInItsAdapterAlone(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)
	root := irtest.RepoRoot(t)
	adapter := filepath.Join(root, "compilers", "openapi", "internal", "statement", "model.go")

	var found bool
	for _, loadedPackage := range pkgs {
		if loadedPackage.Types.Path() != statementPackage {
			continue
		}
		found = true
		outside, inside := modelSelections(loadedPackage, parserSchema, adapter)
		assert.Positive(t, inside, "model.go reads none of the parser's schema types")
		for member, at := range outside {
			for _, pos := range at {
				t.Errorf("statement selects %s on a parser schema at %s: only model.go reads the model",
					member, relPosition(pos, root))
			}
		}
	}
	require.True(t, found, "%s is no production package", statementPackage)
}

// plantedHolderMid is a package that names the planted model in the types its
// functions return, and a generic container that holds it only as a type
// argument.
const plantedHolderMid = `package mid

import "lib"

type Box[T any] struct{ v T }

func Boxed() Box[*lib.Schema]               { return Box[*lib.Schema]{} }
func Slice() []*lib.Schema                  { return nil }
func Wrapped() map[string]*lib.JSONSchema[int] { return nil }
func Make() func() *lib.Schema              { return nil }
func Pred() func(*lib.Schema) bool          { return nil }
func Plain() int                            { return 0 }
func Look() lib.Other                       { return lib.Other{} }
func Names() []string                       { return nil }
func Pair() Box[lib.Other]                  { return Box[lib.Other]{} }
`

// plantedHolder holds the planted model with no import of it: each value comes
// from a call and is never named, and each line that holds one is marked,
// beside the look-alikes that hold none.
const plantedHolder = `package q

import "mid"

func use() {
	_ = mid.Boxed() // holds
	_ = mid.Slice() // holds
	_ = mid.Wrapped() // holds
	_ = mid.Make() // holds
	_ = mid.Pred() // holds
	_ = mid.Plain()
	_ = mid.Look()
	_ = mid.Names()
	_ = mid.Pair()
	inferred := mid.Boxed() // holds
	_ = inferred // holds
}
`

// TestHoldingExpressions_PlantedHoldersAreFound checks a package that never
// imports the planted model and requires exactly the lines its markers name,
// one for each way a type is built from the model. Without it, a counter that
// followed no type argument, or no signature, would pass the tree.
func TestHoldingExpressions_PlantedHoldersAreFound(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	parse := func(name, src string) *ast.File {
		f, err := parser.ParseFile(fset, name, src, 0)
		require.NoError(t, err)
		return f
	}
	checked := map[string]*types.Package{}
	imports := importerFunc(func(path string) (*types.Package, error) {
		if pkg, ok := checked[path]; ok {
			return pkg, nil
		}
		return importer.Default().Import(path)
	})
	check := func(path string, files []*ast.File, info *types.Info) *types.Package {
		pkg, err := (&types.Config{Importer: imports}).Check(path, fset, files, info)
		require.NoError(t, err)
		checked[path] = pkg
		return pkg
	}
	check("lib", []*ast.File{parse("lib.go", plantedModel)}, nil)
	check("mid", []*ast.File{parse("mid.go", plantedHolderMid)}, nil)
	files := []*ast.File{parse("q.go", plantedHolder)}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	pkg := check("q", files, info)

	var want, got []int
	for i, line := range strings.Split(plantedHolder, "\n") {
		if strings.Contains(line, "// holds") {
			want = append(want, i+1)
		}
	}
	for _, pos := range holdingExpressions(&decls.Package{Types: pkg, Info: info, Syntax: files, Fset: fset},
		schemaModel{pkg: "lib", names: []string{"Schema", "JSONSchema"}}) {
		if !slices.Contains(got, pos.Line) {
			got = append(got, pos.Line)
		}
	}
	require.NotEmpty(t, want)
	assert.Equal(t, want, got)
}
