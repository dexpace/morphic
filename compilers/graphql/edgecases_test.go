package graphql

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/dexpace/morphic/ir"
)

// newTestLowerer builds a minimal lowerer for exercising internal helpers in
// isolation, with the maps the helpers assume to be non-nil.
func newTestLowerer() *lowerer {
	return &lowerer{
		out:         &ir.Document{Types: ir.TypeRegistry{}},
		byPointer:   make(map[string]ir.TypeID),
		srcIndex:    make(map[*ast.Source]int),
		defs:        make(map[string]*mergedDef),
		unknownRefs: make(map[string]bool),
	}
}

func TestCanonicalWords_Boundaries(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"firstName":  "first_name",
		"user_id":    "user_id",
		"kebab-case": "kebab_case",
		"with space": "with_space",
		"HTTPServer": "http_server",
		"v2Point":    "v_2_point",
		"key2":       "key_2",
		"ID":         "id",
		"":           "",
	}
	for in, want := range cases {
		assert.Equal(t, want, canonicalWords(in), "canonicalWords(%q)", in)
	}
}

func TestPtr_EmptyIsBlank(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", ptr())
	assert.Equal(t, "/a/b~1c", ptr("a", "b/c"))
}

func TestSrcOf_Fallbacks(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	assert.Equal(t, 0, l.srcOf(nil), "nil position resolves to source 0")
	assert.Equal(t, 0, l.srcOf(&ast.Position{}), "position without a source resolves to 0")
	known := &ast.Source{Name: "a"}
	l.srcIndex[known] = 3
	assert.Equal(t, 3, l.srcOf(&ast.Position{Src: known}))
	assert.Equal(t, 0, l.srcOf(&ast.Position{Src: &ast.Source{Name: "b"}}), "unknown source resolves to 0")
}

func TestPositionProvenance_NilAndNoSource(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.Provenance{}, positionProvenance(nil, nil))
	prov := positionProvenance(&ast.Position{Line: 2, Column: 5}, map[*ast.Source]int{})
	assert.Equal(t, "2:5", prov.Pointer)
	assert.Equal(t, 0, prov.Source)
}

func TestMemberPos_FallsBackToDefinition(t *testing.T) {
	t.Parallel()
	defPos := &ast.Position{Line: 1}
	memPos := &ast.Position{Line: 2}
	d := &ast.Definition{Position: defPos, TypePositions: []*ast.Position{memPos}}
	assert.Same(t, memPos, memberPos(d, 0), "recorded member position is used")
	assert.Same(t, defPos, memberPos(d, 1), "missing member position falls back to the definition")
	bare := &ast.Definition{Position: defPos}
	assert.Same(t, defPos, memberPos(bare, 0), "no member positions falls back to the definition")
}

func TestStringArg_Absent(t *testing.T) {
	t.Parallel()
	d := &ast.Directive{Name: "x"}
	assert.Equal(t, "", stringArg(d, "missing"))
}

func TestIrValue_AllKinds(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	prov := ir.Provenance{}
	assert.Nil(t, l.irValue(nil, prov), "a nil literal yields no value")
	assert.Equal(t, ir.ValueNull, l.irValue(&ast.Value{Kind: ast.NullValue}, prov).Kind)
	assert.False(t, l.irValue(&ast.Value{Kind: ast.BooleanValue, Raw: "false"}, prov).Bool)
	assert.Equal(t, "s", l.irValue(&ast.Value{Kind: ast.StringValue, Raw: "s"}, prov).Str)
	assert.Equal(t, "b", l.irValue(&ast.Value{Kind: ast.BlockValue, Raw: "b"}, prov).Str)
	assert.Equal(t, ir.ValueSymbol, l.irValue(&ast.Value{Kind: ast.EnumValue, Raw: "A"}, prov).Kind)
	num := l.irValue(&ast.Value{Kind: ast.IntValue, Raw: "42"}, prov)
	assert.Equal(t, ir.BigVal("42"), num.Num)
	list := l.irValue(&ast.Value{Kind: ast.ListValue, Children: ast.ChildValueList{
		{Value: &ast.Value{Kind: ast.IntValue, Raw: "1"}},
	}}, prov)
	require.Equal(t, ir.ValueList, list.Kind)
	require.Len(t, list.List, 1)
	obj := l.irValue(&ast.Value{Kind: ast.ObjectValue, Children: ast.ChildValueList{
		{Name: "k", Value: &ast.Value{Kind: ast.StringValue, Raw: "v"}},
	}}, prov)
	require.Equal(t, ir.ValueObject, obj.Kind)
	require.Len(t, obj.Object, 1)
	assert.Equal(t, "k", obj.Object[0].Name)
}

func TestIrValue_InvalidNumberDiagnoses(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	v := l.irValue(&ast.Value{Kind: ast.IntValue, Raw: "0x10"}, ir.Provenance{})
	assert.Equal(t, ir.ValueNull, v.Kind, "a non-decimal literal degrades to null")
	require.Len(t, l.diags, 1)
	assert.Equal(t, codeInvalidValue, l.diags[0].Code)
}

func TestIrValue_UnknownKindDiagnoses(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	v := l.irValue(&ast.Value{Kind: ast.Variable, Raw: "x"}, ir.Provenance{})
	assert.Equal(t, ir.ValueNull, v.Kind, "a variable literal has no place in an SDL constant")
	require.Len(t, l.diags, 1)
	assert.Equal(t, codeDegradedConstruct, l.diags[0].Code)
}

func TestIrValue_MaxDepthDiagnoses(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	deep := nestedList(maxValueDepth + 2)
	v := l.irValue(deep, ir.Provenance{})
	assert.Equal(t, ir.ValueList, v.Kind)
	codes := make([]string, 0, len(l.diags))
	for _, d := range l.diags {
		codes = append(codes, d.Code)
	}
	assert.Contains(t, codes, codeDegradedConstruct, "over-deep nesting is bounded and diagnosed")
}

// nestedList builds a list literal nested depth levels deep, terminating in an
// integer.
func nestedList(depth int) *ast.Value {
	v := &ast.Value{Kind: ast.IntValue, Raw: "1"}
	for range depth {
		v = &ast.Value{Kind: ast.ListValue, Children: ast.ChildValueList{{Value: v}}}
	}
	return v
}

func TestValueJSON_AllKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   *ast.Value
		want string
	}{
		{nil, "null"},
		{&ast.Value{Kind: ast.NullValue}, "null"},
		{&ast.Value{Kind: ast.BooleanValue, Raw: "true"}, "true"},
		{&ast.Value{Kind: ast.BooleanValue, Raw: "false"}, "false"},
		{&ast.Value{Kind: ast.IntValue, Raw: "7"}, "7"},
		{&ast.Value{Kind: ast.IntValue, Raw: "0xFF"}, `"0xFF"`},
		{&ast.Value{Kind: ast.StringValue, Raw: "hi"}, `"hi"`},
		{&ast.Value{Kind: ast.EnumValue, Raw: "A"}, `"A"`},
		{&ast.Value{Kind: ast.Variable, Raw: "x"}, `"$x"`},
	}
	for _, tc := range cases {
		assert.JSONEq(t, tc.want, string(valueJSON(tc.in)))
	}
}

func TestValueJSON_ListAndObject(t *testing.T) {
	t.Parallel()
	list := &ast.Value{Kind: ast.ListValue, Children: ast.ChildValueList{
		{Value: &ast.Value{Kind: ast.IntValue, Raw: "1"}},
		{Value: &ast.Value{Kind: ast.StringValue, Raw: "a"}},
	}}
	assert.JSONEq(t, `[1,"a"]`, string(valueJSON(list)))
	obj := &ast.Value{Kind: ast.ObjectValue, Children: ast.ChildValueList{
		{Name: "n", Value: &ast.Value{Kind: ast.IntValue, Raw: "2"}},
		{Name: "m", Value: &ast.Value{Kind: ast.BooleanValue, Raw: "true"}},
	}}
	assert.JSONEq(t, `{"n":2,"m":true}`, string(valueJSON(obj)))
}

func TestNamedRef_BuiltInScalars(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	assert.Equal(t, primTypeID(ir.PrimInt32), l.namedRef("Int", nil))
	assert.Equal(t, primTypeID(ir.PrimFloat64), l.namedRef("Float", nil))
	assert.Equal(t, primTypeID(ir.PrimString), l.namedRef("String", nil))
	assert.Equal(t, primTypeID(ir.PrimBool), l.namedRef("Boolean", nil))
	assert.Equal(t, namedTypeID(ptr("scalars", "ID")), l.namedRef("ID", nil))
}

func TestValueJSON_UnknownKindIsNull(t *testing.T) {
	t.Parallel()
	assert.JSONEq(t, "null", string(valueJSON(&ast.Value{Kind: ast.ValueKind(99)})))
}

func TestValueJSON_MaxDepthIsNull(t *testing.T) {
	t.Parallel()
	assert.JSONEq(t, "null", string(valueJSONAt(nestedList(1), maxValueDepth+1)))
}

func TestBuildDefinition_UnknownKindDegradesToAny(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	md := &mergedDef{def: &ast.Definition{Kind: ast.DefinitionKind("bogus"), Name: "X"}}
	td := l.buildDefinition(md)
	_, ok := td.(*ir.Any)
	assert.True(t, ok, "an unrecognized definition kind degrades to Any")
	require.Len(t, l.diags, 1)
	assert.Equal(t, codeDegradedConstruct, l.diags[0].Code)
}

func TestTypeRef_NilIsAny(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	ref := l.typeRef(nil, "/p")
	assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target)
}

func TestTypeRef_MaxDepthDegrades(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	l.depth = maxTypeDepth
	ref := l.typeRef(ast.ListType(ast.NamedType("Int", nil), nil), "/p")
	assert.Equal(t, ir.TypeID("t/prim/any"), ref.Target, "over-deep type nesting is bounded")
	require.NotEmpty(t, l.diags)
	assert.Equal(t, codeDegradedConstruct, l.diags[0].Code)
}

func TestApplyInaccessibleType_SetsInternalAccess(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	var c ir.TypeCommon
	l.applyInaccessibleType(&c, ast.DirectiveList{{Name: "inaccessible"}})
	assert.Equal(t, "internal", c.Access)
}

func TestReportUnknownType_Deduplicates(t *testing.T) {
	t.Parallel()
	l := newTestLowerer()
	l.reportUnknownType("Missing", nil)
	l.reportUnknownType("Missing", nil)
	assert.Len(t, l.diags, 1, "a dangling name is reported once")
}

func TestParseDiags_NonGqlError(t *testing.T) {
	t.Parallel()
	diags := parseDiags(errors.New("boom"))
	require.Len(t, diags, 1)
	assert.Equal(t, codeParse, diags[0].Code)
	assert.Contains(t, diags[0].Message, "boom")
}

func TestGqlErrProvenance_NoLocations(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.Provenance{}, gqlErrProvenance(&gqlerror.Error{}))
	prov := gqlErrProvenance(&gqlerror.Error{Locations: []gqlerror.Location{{Line: 4, Column: 2}}})
	assert.Equal(t, "4:2", prov.Pointer)
}

func TestParseAll_RecoversFromPanic(t *testing.T) {
	t.Parallel()
	_, err := parseAll(func(int, ...*ast.Source) (*ast.SchemaDocument, error) {
		panic("kaboom")
	}, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errParse), "a parser panic is recovered as errParse")
}

func TestLoad_ParserPanicIsGoError(t *testing.T) {
	t.Parallel()
	panicky := func(int, ...*ast.Source) (*ast.SchemaDocument, error) { panic("nope") }
	ld, diags, err := load(panicky, nil, nil, nil)
	require.Error(t, err)
	assert.Nil(t, ld)
	assert.Nil(t, diags, "a panic is a Go error, not a diagnostic")
}

func TestDirectiveDefJSON_WithDescription(t *testing.T) {
	t.Parallel()
	def := &ast.DirectiveDefinition{
		Name:         "auth",
		Description:  "guards a field",
		IsRepeatable: true,
		Locations:    []ast.DirectiveLocation{ast.LocationFieldDefinition},
		Arguments: ast.ArgumentDefinitionList{
			{Name: "role", Type: ast.NonNullNamedType("String", nil), DefaultValue: &ast.Value{Kind: ast.StringValue, Raw: "admin"}},
		},
	}
	raw := directiveDefJSON(def)
	assert.JSONEq(t,
		`{"name":"auth","description":"guards a field","repeatable":true,"locations":["FIELD_DEFINITION"],"arguments":[{"name":"role","type":"String!","defaultValue":"admin"}]}`,
		string(raw))
}
