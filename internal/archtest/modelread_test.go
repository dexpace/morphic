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

// modelReadBaseline is how many times each production package, keyed by its
// repo-relative path, selects each member of the parser library's schema
// types outside the statement adapter, compilers/openapi/internal/statement's
// model.go, and names a bridge back to them (bridgeKey).
// TestModelReads_HeldToTheBaseline fails when a count rises, since
// each new read is a second reading of the source beside the statement, and
// when one falls until the count here is lowered to match, so the map only
// shrinks as the readers move onto the statement (GitHub #1991).
var modelReadBaseline = map[string]map[string]int{
	"compilers/openapi/internal/annotation": {
		"Deprecated":               1,
		"GetAdditionalProperties":  1,
		"GetBool":                  2,
		"GetContains":              1,
		"GetCore":                  1,
		"GetDependentSchemas":      1,
		"GetDescription":           1,
		"GetDiscriminator":         1,
		"GetExample":               1,
		"GetExamples":              1,
		"GetExclusiveMaximum":      1,
		"GetExclusiveMinimum":      1,
		"GetExtensions":            1,
		"GetExternalDocs":          2,
		"GetFormat":                1,
		"GetItems":                 1,
		"GetMaxContains":           1,
		"GetMinContains":           1,
		"GetNot":                   1,
		"GetPropertyNames":         1,
		"GetSchema":                1,
		"GetTitle":                 1,
		"GetUnevaluatedItems":      1,
		"GetUnevaluatedProperties": 2,
		"GetXML":                   1,
		"IsBool":                   3,
		"IsReference":              1,
		"ReadOnly":                 1,
		"WriteOnly":                1,
		"statement.Parsed":         14,
	},
	"compilers/openapi/internal/bind/typed": {
		"GetCore":     1,
		"GetRootNode": 1,
		"GetSchema":   2,
		"IsBool":      3,
		"Ref":         1,
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
		"GetRootNode":              2,
		"GetSchema":                4,
		"GetThen":                  1,
		"GetType":                  6,
		"GetUnevaluatedProperties": 2,
		"IsSchema":                 2,
		"statement.Parsed":         14,
	},
	"compilers/openapi/internal/resolve": {
		"GetSchema": 1,
		"IsBool":    1,
	},
	"compilers/openapi/internal/schema": {
		"GetAdditionalProperties":  1,
		"GetAllOf":                 20,
		"GetAnyOf":                 14,
		"GetConst":                 12,
		"GetContains":              1,
		"GetContentSchema":         1,
		"GetDefault":               2,
		"GetDependentSchemas":      1,
		"GetDeprecated":            4,
		"GetDescription":           1,
		"GetDiscriminator":         1,
		"GetElse":                  1,
		"GetEnum":                  8,
		"GetExample":               1,
		"GetExamples":              1,
		"GetExtensions":            2,
		"GetExternalDocs":          1,
		"GetFormat":                2,
		"GetIf":                    2,
		"GetItems":                 2,
		"GetMaxContains":           1,
		"GetMinContains":           1,
		"GetNot":                   2,
		"GetOneOf":                 14,
		"GetPatternProperties":     1,
		"GetPrefixItems":           1,
		"GetProperties":            6,
		"GetPropertyNames":         1,
		"GetRef":                   1,
		"GetRequired":              5,
		"GetSchema":                15,
		"GetThen":                  1,
		"GetTitle":                 1,
		"GetType":                  5,
		"GetUnevaluatedItems":      1,
		"GetUnevaluatedProperties": 2,
		"GetXML":                   1,
		"IsBool":                   3,
		"IsSchema":                 1,
		"Nullable":                 8,
		"Ref":                      4,
		"statement.Parsed":         166,
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

// bridgeKey is the key modelReadBaseline counts each use of the statement's
// bridges under: a function handing back the parser's value a statement was
// read from, which a reader not yet reading the statement reads the model
// through as surely as by a selection on it.
const bridgeKey = "statement.Parsed"

// statementPackage is the package the bridges are declared in, and bridges
// names them.
const statementPackage = module + "/compilers/openapi/internal/statement"

var bridges = []string{"Parsed", "ParsedAt"}

// bridgeUses returns where p names one of the statement's bridges.
func bridgeUses(p *decls.Package) []token.Position {
	var out []token.Position
	for id, obj := range p.Info.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != statementPackage || !slices.Contains(bridges, fn.Name()) {
			continue
		}
		out = append(out, p.Fset.Position(id.Pos()))
	}
	slices.SortFunc(out, func(a, b token.Position) int {
		if c := strings.Compare(a.Filename, b.Filename); c != 0 {
			return c
		}
		return a.Offset - b.Offset
	})
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

// modelSelections returns where p selects a member of one of m's types
// (reaches), by member, whatever type declares the member, so one promoted
// from an embedded model counts: a field, a method value or call, and a method
// expression. A call through an interface or a type parameter and a library
// function handed a schema select no member of m's types, and holds does not
// recognize a type defined over one, such as `type s oas3.Schema`: none of
// these is counted. It leaves out those in the file adapter names and returns
// how many there were.
func modelSelections(p *decls.Package, m schemaModel, adapter string) (outside map[string][]token.Position, inAdapter int) {
	outside = map[string][]token.Position{}
	for expr, sel := range p.Info.Selections {
		if !m.reaches(sel) {
			continue
		}
		pos := p.Fset.Position(expr.Sel.Pos())
		if pos.Filename == adapter {
			inAdapter++
			continue
		}
		outside[expr.Sel.Name] = append(outside[expr.Sel.Name], pos)
	}
	for _, at := range outside {
		slices.SortFunc(at, func(a, b token.Position) int {
			if c := strings.Compare(a.Filename, b.Filename); c != 0 {
				return c
			}
			return a.Offset - b.Offset
		})
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
	for _, p := range pkgs {
		rel := strings.TrimPrefix(p.Types.Path(), module+"/")
		outside, inside := modelSelections(p, parserSchema, adapter)
		inAdapter += inside
		if at := bridgeUses(p); len(at) > 0 {
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
