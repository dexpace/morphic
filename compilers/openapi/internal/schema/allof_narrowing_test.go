package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestAllOf_AnInadmissibleBroadValueIsKeptInEitherOrder pins that the broader
// branch of a narrowing, whichever comes first, never lands a default the
// narrower kind refuses on the merged field: it is kept verbatim at its own
// branch with one info, so the two orders give the same field and the same
// diagnostics.
func TestAllOf_AnInadmissibleBroadValueIsKeptInEitherOrder(t *testing.T) {
	t.Parallel()
	const broad = "            p: {type: number, default: 1.5}\n"
	const narrow = "            p: {type: integer}\n"
	branch := "        - type: object\n          properties:\n"
	for _, tc := range []struct{ name, first, second, broadPtr string }{
		{"broad first", broad, narrow, "/components/schemas/Ranged/allOf/0/properties/p"},
		{"narrow first", narrow, broad, "/components/schemas/Ranged/allOf/1/properties/p"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    Ranged:\n      allOf:\n"+
				branch+tc.first+branch+tc.second))
			openapitest.RequireNoErrorDiags(t, diags)
			m, ok := doc.Types[componentID("Ranged")].(*ir.Model)
			require.True(t, ok, "Ranged should be a model")
			require.Len(t, m.Properties, 1)

			p := m.Properties[0]
			assert.Equal(t, ir.TypeID("t/prim/integer"), p.Type.Target)
			assert.Nil(t, p.Default, "the refused default does not land")
			require.Len(t, diags, 1, "one info and nothing else")
			assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
			assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
			assert.Equal(t, jsontext.Pointer(tc.broadPtr), diags[0].Provenance.Pointer)
			entry, ok := p.Unmodeled["openapi:conflicting-redeclaration"+tc.broadPtr]
			require.True(t, ok, "the broad branch is kept at its own pointer; got %v", p.Unmodeled)
			assert.JSONEq(t, `{"type":"number","default":1.5}`, string(entry.Value))
			assert.Len(t, p.Unmodeled, 1)
		})
	}
}

// TestAllOf_ANullOnARefOverTheTopTypeIsKept folds two $refs to a scalar over
// the top type, one writing default: null. The top type admits null, a fact
// only the Base walk reaches, so the default lands and nothing is reported or
// kept aside, in either order.
func TestAllOf_ANullOnARefOverTheTopTypeIsKept(t *testing.T) {
	t.Parallel()
	const withNull = "            p: {$ref: '#/components/schemas/Y', default: null}\n"
	const plain = "            p: {$ref: '#/components/schemas/Y'}\n"
	branch := "        - type: object\n          properties:\n"
	for _, tc := range []struct{ name, first, second string }{
		{"null first", withNull, plain},
		{"null second", plain, withNull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    Y: {description: free}\n    Held:\n      allOf:\n"+
				branch+tc.first+branch+tc.second))
			assert.Empty(t, diags, "nothing is refused")
			m, ok := doc.Types[componentID("Held")].(*ir.Model)
			require.True(t, ok, "Held should be a model")
			require.Len(t, m.Properties, 1)
			p := m.Properties[0]
			require.NotNil(t, p.Default, "the null default lands")
			assert.Equal(t, ir.ValueNull, p.Default.Kind)
			assert.Empty(t, p.Unmodeled)
		})
	}
}

// TestAllOf_NonBareNarrowingIsOrderInvariant pins that a redeclaration with a
// side that is not a bare primitive settles on its narrower side whichever
// branch is written first, with no conflict or degradation reported and
// nothing kept.
func TestAllOf_NonBareNarrowingIsOrderInvariant(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, extra, narrow, broad string
		kind                       ir.TypeKind
	}{
		{"an enum over its kind", "", "{type: integer, enum: [1, 2]}", "{type: integer}", ir.KindEnum},
		{"a const over its kind", "", "{const: 1}", "{type: integer}", ir.KindLiteral},
		{"an untyped, described schema", "", "{type: integer}", "{description: the id}", ir.KindPrimitive},
		{"a list of a narrower kind", "", "{type: array, items: {type: integer, format: int32}}",
			"{type: array, items: {type: integer}}", ir.KindList},
		{"a map of a narrower kind", "", "{type: object, additionalProperties: {type: integer, format: int32}}",
			"{type: object, additionalProperties: {type: integer}}", ir.KindMap},
		{"an unknown format over its kind", "", "{type: string, format: hex-token}", "{type: string}", ir.KindScalar},
		{"a ref declared before the schema", "    Id: {type: integer, format: int32}\n",
			"{$ref: '#/components/schemas/Id'}", "{type: integer}", ir.KindScalar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			types := make([]ir.TypeDef, 0, 2)
			for _, order := range [][2]string{{tc.narrow, tc.broad}, {tc.broad, tc.narrow}} {
				doc, diags := schema.LowerSpec(t, nonBareSpec(tc.extra, order[0], order[1]))
				openapitest.RequireNoErrorDiags(t, diags)
				m, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok)
				require.Len(t, m.Properties, 1)
				p := m.Properties[0]
				types = append(types, doc.Types[p.Type.Target])
				assert.Equal(t, tc.kind, doc.Types[p.Type.Target].Kind(), "the narrower declaration's type wins")
				assert.Empty(t, p.Unmodeled, "nothing is kept")
				for _, d := range diags {
					assert.NotContains(t, []string{diag.ConflictingRedecl, diag.DegradedConstruct}, d.Code, d.Message)
				}
			}
			assert.Empty(t, cmp.Diff(types[0], types[1],
				cmpopts.IgnoreFields(ir.TypeCommon{}, "ID", "Name", "Provenance"),
				cmpopts.IgnoreFields(ir.EnumMember{}, "ID")),
				"the winner does not depend on branch order")
		})
	}
}

// TestAllOf_AnUnknownFormatWinsInEitherOrder pins GitHub #782: a format that
// hoists a Scalar over the bare kind narrows it whichever branch is first, and
// the format's own details land on the field rather than being parked.
func TestAllOf_AnUnknownFormatWinsInEitherOrder(t *testing.T) {
	t.Parallel()
	plain := "{type: string}"
	formatted := "{type: string, format: hex-token, minLength: 8, default: x, description: the token}"
	for i, order := range [][2]string{{plain, formatted}, {formatted, plain}} {
		doc, diags := schema.LowerSpec(t, nonBareSpec("", order[0], order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]

		assert.Equal(t, ir.KindScalar, doc.Types[p.Type.Target].Kind(), "the format's scalar types the field (order %d)", i)
		require.NotNil(t, p.Default, "order %d", i)
		assert.Equal(t, "x", p.Default.Str, "order %d", i)
		require.NotNil(t, p.Constraints, "order %d", i)
		require.NotNil(t, p.Constraints.MinLength, "order %d", i)
		assert.Equal(t, int64(8), *p.Constraints.MinLength, "order %d", i)
		sc, ok := doc.Types[p.Type.Target].(*ir.Scalar)
		require.True(t, ok, "order %d", i)
		assert.Equal(t, "the token", sc.Docs.Description, "the description stays on the scalar that wrote it (order %d)", i)
		assert.Empty(t, p.Unmodeled, "nothing is parked (order %d)", i)
		assert.Empty(t, diags, "order %d", i)
	}
}

// TestAllOf_TheTopTypeAndANullableKindMeetAsNullableInEitherOrder pins that `{}`
// against `type: [integer, "null"]` is a nullable integer whichever is first.
func TestAllOf_TheTopTypeAndANullableKindMeetAsNullableInEitherOrder(t *testing.T) {
	t.Parallel()
	top, nullable := "{}", `{type: [integer, "null"]}`
	for i, order := range [][2]string{{top, nullable}, {nullable, top}} {
		doc, diags := schema.LowerSpec(t, nonBareSpec("", order[0], order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]

		assert.Equal(t, ir.TypeRef{Target: "t/prim/integer", Nullable: true}, p.Type, "order %d", i)
		assert.Empty(t, p.Unmodeled, "order %d", i)
		assert.Empty(t, diags, "order %d", i)
	}
}

// TestAllOf_ADefaultTheNarrowerNodeRefusesIsKeptInEitherOrder pins that the
// broader declaration's default is judged against a winner that is not a bare
// primitive: one the enum does not admit is not adopted, and the declaration
// that wrote it is kept at its own pointer, whichever branch it is.
func TestAllOf_ADefaultTheNarrowerNodeRefusesIsKeptInEitherOrder(t *testing.T) {
	t.Parallel()
	enum, broad := "{type: integer, enum: [1, 2]}", "{type: integer, default: 5}"
	for i, tc := range []struct {
		order [2]string
		kept  string
	}{
		{[2]string{enum, broad}, "/components/schemas/T/allOf/1/properties/v"},
		{[2]string{broad, enum}, "/components/schemas/T/allOf/0/properties/v"},
	} {
		doc, diags := schema.LowerSpec(t, nonBareSpec("", tc.order[0], tc.order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]

		assert.Equal(t, ir.KindEnum, doc.Types[p.Type.Target].Kind(), "the enum wins (order %d)", i)
		assert.Nil(t, p.Default, "the refused default is not adopted (order %d)", i)
		require.Len(t, p.Unmodeled, 1, "order %d", i)
		assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration"+tc.kept, "order %d", i)
		require.Len(t, diags, 1, "order %d", i)
		assert.Equal(t, diag.DegradedConstruct, diags[0].Code, "order %d", i)
		assert.Equal(t, ir.SeverityInfo, diags[0].Severity, "order %d", i)
	}
}

// TestAllOf_AWinnersConstraintsRefuseADefaultInEitherOrder pins that the
// winning node holds the broader declaration's default to its constraints as
// well as its kind: a string past a component's maxLength, or a list past its
// maxItems, is not adopted, and the declaration that wrote it is kept at its
// own pointer, whichever branch it is and wherever the component is declared.
func TestAllOf_AWinnersConstraintsRefuseADefaultInEitherOrder(t *testing.T) {
	t.Parallel()
	named := "{$ref: '#/components/schemas/C'}"
	lowerings := map[string]func(extra, a, b string) (*ir.Document, []ir.Diagnostic){
		"declared before": func(extra, a, b string) (*ir.Document, []ir.Diagnostic) {
			return schema.LowerSpec(t, nonBareSpec(extra, a, b))
		},
		"declared after": func(extra, a, b string) (*ir.Document, []ir.Diagnostic) { return settledSpec(t, extra, a, b) },
	}
	for _, tc := range []struct{ name, component, broad string }{
		{"a string past maxLength", "    C: {type: string, maxLength: 3}\n", "{type: string, default: hello}"},
		{"a list past maxItems", "    C: {type: array, items: {type: integer, format: int32}, maxItems: 2}\n",
			"{type: array, items: {type: integer}, default: [1, 2, 3, 4]}"},
	} {
		for where, lower := range lowerings {
			for _, order := range []struct{ a, b, kept string }{
				{named, tc.broad, "/components/schemas/T/allOf/1/properties/v"},
				{tc.broad, named, "/components/schemas/T/allOf/0/properties/v"},
			} {
				doc, diags := lower(tc.component, order.a, order.b)
				at := tc.name + ", " + where + ", kept " + order.kept
				openapitest.RequireNoErrorDiags(t, diags)
				m, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok, at)
				require.Len(t, m.Properties, 1, at)
				p := m.Properties[0]

				assert.Equal(t, componentID("C"), p.Type.Target, "the component wins: %s", at)
				assert.Nil(t, p.Default, "the refused default is not adopted: %s", at)
				require.Len(t, p.Unmodeled, 1, at)
				assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration"+order.kept, at)
				require.Len(t, diags, 1, at)
				assert.Equal(t, diag.DegradedConstruct, diags[0].Code, at)
				assert.Equal(t, jsontext.Pointer(order.kept), diags[0].Provenance.Pointer, at)
			}
		}
	}
}

// TestAllOf_TwoEqualListsRefuseANullTheMergeDoesNotAdmit pins that two lists of
// one element, which are two targets, settle on a non-nullable list, so a null
// default the nullable declaration wrote is not adopted and the declaration is
// kept at its own pointer, whichever branch it is.
func TestAllOf_TwoEqualListsRefuseANullTheMergeDoesNotAdmit(t *testing.T) {
	t.Parallel()
	nullable := `{type: [array, "null"], items: {type: integer}, default: null}`
	plain := "{type: array, items: {type: integer}}"
	for i, tc := range []struct {
		order [2]string
		kept  string
	}{
		{[2]string{nullable, plain}, "/components/schemas/T/allOf/0/properties/v"},
		{[2]string{plain, nullable}, "/components/schemas/T/allOf/1/properties/v"},
	} {
		doc, diags := schema.LowerSpec(t, nonBareSpec("", tc.order[0], tc.order[1]))
		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]

		require.Equal(t, ir.KindList, doc.Types[p.Type.Target].Kind(), "order %d", i)
		assert.False(t, p.Type.Nullable, "order %d", i)
		assert.Nil(t, p.Default, "the refused null is not adopted (order %d)", i)
		require.Len(t, p.Unmodeled, 1, "order %d", i)
		assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration"+tc.kept, "order %d", i)
		require.Len(t, diags, 1, "order %d", i)
		assert.Equal(t, diag.DegradedConstruct, diags[0].Code, "order %d", i)
		assert.Equal(t, ir.SeverityInfo, diags[0].Severity, "order %d", i)
		assert.Equal(t, jsontext.Pointer(tc.kept), diags[0].Provenance.Pointer, "order %d", i)
	}
}

// TestAllOf_IdenticalNodesAreOneType compiles a field written twice with the
// same enum, const or format, in either order. Two inline copies are one type,
// and a component wins over an inline copy of itself: no diagnostic and nothing
// kept. Two components declared alike are two types the source named apart: an
// info, with the first kept and the second's $ref kept verbatim.
func TestAllOf_IdenticalNodesAreOneType(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"{type: string, enum: [a, b]}",
		"{const: a}",
		"{type: string, format: hex}",
		"{type: string, enum: [a, b], x-foo: 1}",
		"{type: array, items: {type: integer, format: int32}}",
		"{type: object, additionalProperties: {type: integer}}",
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			extra := "    P: " + body + "\n    Q: " + body + "\n"
			p, q := "{$ref: '#/components/schemas/P'}", "{$ref: '#/components/schemas/Q'}"
			for i, tc := range []struct {
				order [2]string
				win   ir.TypeID // the type that stands
				same  bool
			}{
				{[2]string{body, body}, "t/anon/components/schemas/T/allOf/0/properties/v", true},
				{[2]string{p, body}, componentID("P"), true},
				{[2]string{body, p}, componentID("P"), true},
				{[2]string{p, q}, componentID("P"), false},
				{[2]string{q, p}, componentID("Q"), false},
			} {
				doc, diags := schema.LowerSpec(t, nonBareSpec(extra, tc.order[0], tc.order[1]))
				m, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok, "order %d", i)
				require.Len(t, m.Properties, 1, "order %d", i)
				prop := m.Properties[0]
				assert.Equal(t, tc.win, prop.Type.Target, "order %d", i)
				if tc.same {
					assert.Empty(t, diags, "order %d", i)
					assert.Empty(t, prop.Unmodeled, "order %d", i)
				} else {
					require.Len(t, diags, 1, "order %d", i)
					assert.Equal(t, diag.DegradedConstruct, diags[0].Code, "order %d", i)
					assert.Len(t, prop.Unmodeled, 1, "the second $ref is kept (order %d)", i)
				}
				for _, name := range []string{"P", "Q"} {
					td, ok := doc.Types[componentID(name)]
					require.True(t, ok, "%s (order %d)", name, i)
					for key, e := range td.Common().Unmodeled {
						assert.True(t, strings.HasPrefix(string(e.Provenance.Pointer), "/components/schemas/"+name+"/"),
							"%s keeps where its %s came from, got %q (order %d)", name, key, e.Provenance.Pointer, i)
					}
				}
			}
		})
	}
}

// TestAllOf_ARefToAnInlineNodeIsDeclaredApart compiles a field redeclared as
// a $ref to a node written inline elsewhere (X/properties/c), in either order.
// That node was declared apart from the field, as a component is: it wins over
// an inline copy of itself, and against a $ref to its twin (X/properties/d) the
// pair is an info, with the first kept and the second's $ref kept verbatim.
func TestAllOf_ARefToAnInlineNodeIsDeclaredApart(t *testing.T) {
	t.Parallel()
	const enum = "{type: string, enum: [a, b]}"
	extra := "    X: {type: object, properties: {c: " + enum + ", d: " + enum + "}}\n"
	c, d := "{$ref: '#/components/schemas/X/properties/c'}", "{$ref: '#/components/schemas/X/properties/d'}"
	at := func(p string) ir.TypeID { return ir.TypeID("t/anon/components/schemas/X/properties/" + p) }
	for i, tc := range []struct {
		order [2]string
		win   ir.TypeID
		same  bool
	}{
		{[2]string{c, enum}, at("c"), true},
		{[2]string{enum, c}, at("c"), true},
		{[2]string{c, d}, at("c"), false},
		{[2]string{d, c}, at("d"), false},
	} {
		doc, diags := schema.LowerSpec(t, nonBareSpec(extra, tc.order[0], tc.order[1]))
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok, "order %d", i)
		require.Len(t, m.Properties, 1, "order %d", i)
		prop := m.Properties[0]
		assert.Equal(t, tc.win, prop.Type.Target, "order %d", i)
		if tc.same {
			assert.Empty(t, diags, "order %d", i)
			assert.Empty(t, prop.Unmodeled, "order %d", i)
			continue
		}
		require.Len(t, diags, 1, "order %d", i)
		assert.Equal(t, diag.DegradedConstruct, diags[0].Code, "order %d", i)
		assert.Len(t, prop.Unmodeled, 1, "the second $ref is kept (order %d)", i)
	}
}

// TestAllOf_ACopyANarrowingKeptIsStillACopy compiles a field declared as {},
// an inline enum and a $ref to a component of the same enum, in every order,
// with the component declared before the composing schema and after it.
// Where {} comes first, the inline enum narrows it and the property holds a node
// written at the second declaration, not the first; it is still an inline copy,
// so the component wins in all six orders with nothing reported or kept.
func TestAllOf_ACopyANarrowingKeptIsStillACopy(t *testing.T) {
	t.Parallel()
	const enum = "{type: string, enum: [a, b]}"
	decls := []string{"{}", enum, "{$ref: '#/components/schemas/Color'}"}
	for _, colorLast := range []bool{false, true} {
		for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
			var branches strings.Builder
			for _, i := range order {
				branches.WriteString("        - type: object\n          properties: {v: " + decls[i] + "}\n")
			}
			color, composed := "    Color: "+enum+"\n", "    T:\n      allOf:\n"+branches.String()
			schemas := color + composed
			if colorLast {
				schemas = composed + color
			}
			doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec(schemas))
			m, ok := doc.Types[componentID("T")].(*ir.Model)
			require.True(t, ok, "order %v, Color last %t", order, colorLast)
			require.Len(t, m.Properties, 1, "order %v, Color last %t", order, colorLast)
			assert.Equal(t, componentID("Color"), m.Properties[0].Type.Target, "order %v, Color last %t", order, colorLast)
			assert.Empty(t, diags, "order %v, Color last %t", order, colorLast)
			assert.Empty(t, m.Properties[0].Unmodeled, "order %v, Color last %t", order, colorLast)
		}
	}
}

// TestAllOf_AListOfAComponentWinsOverAListOfItsCopy compiles a field redeclared
// as a list of a component and as a list of an inline copy of it, in either
// order. The copy is written under the field's own declaration, so the list
// holding the component wins in both orders with nothing reported or kept.
func TestAllOf_AListOfAComponentWinsOverAListOfItsCopy(t *testing.T) {
	t.Parallel()
	const enum = "{type: string, enum: [a, b]}"
	ofRef, ofCopy := "{type: array, items: {$ref: '#/components/schemas/Color'}}", "{type: array, items: "+enum+"}"
	for i, order := range [][2]string{{ofRef, ofCopy}, {ofCopy, ofRef}} {
		doc, diags := schema.LowerSpec(t, nonBareSpec("    Color: "+enum+"\n", order[0], order[1]))
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok, "order %d", i)
		require.Len(t, m.Properties, 1, "order %d", i)
		list, ok := doc.Types[m.Properties[0].Type.Target].(*ir.List)
		require.True(t, ok, "the field is a list (order %d)", i)
		assert.Equal(t, componentID("Color"), list.Elem.Target, "order %d", i)
		assert.Empty(t, diags, "order %d", i)
		assert.Empty(t, m.Properties[0].Unmodeled, "order %d", i)
	}
}

// TestAllOf_ARefToASiblingFieldIsDeclaredApart compiles a field v redeclared as
// a $ref to its branch's sibling field vx and as an inline copy of vx, in either
// order. vx's pointer begins with v's but is not under it, so vx was declared
// apart and wins in both orders with nothing reported or kept.
func TestAllOf_ARefToASiblingFieldIsDeclaredApart(t *testing.T) {
	t.Parallel()
	const enum = "{type: string, enum: [a, b]}"
	for i := range 2 {
		vx := fmt.Sprintf("/components/schemas/T/allOf/%d/properties/vx", i)
		branches := [2]string{
			"        - type: object\n          properties: {vx: " + enum + ", v: {$ref: '#" + vx + "'}}\n",
			"        - type: object\n          properties: {v: " + enum + "}\n",
		}
		if i == 1 {
			branches[0], branches[1] = branches[1], branches[0]
		}
		doc, diags := schema.LowerSpec(t, openapitest.ComponentSpec("    T:\n      allOf:\n"+branches[0]+branches[1]))
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok, "order %d", i)
		var v *ir.Property
		for k := range m.Properties {
			if m.Properties[k].WireName == "v" {
				v = &m.Properties[k]
			}
		}
		require.NotNil(t, v, "order %d", i)
		assert.Equal(t, ir.TypeID("t/anon"+vx), v.Type.Target, "order %d", i)
		assert.Empty(t, diags, "order %d", i)
		assert.Empty(t, v.Unmodeled, "order %d", i)
	}
}

// TestAllOf_ARedeclarationIsJudgedOnceTheComponentsHaveLowered pins that a
// field redeclared as two $refs to components declared after the composing
// schema is judged as it is when they are declared before it: two components
// declared alike, or a pair the merge does not narrow, are reported and kept. It
// compiles three arms: the components declared before the schema, and after it
// in either order of the two (#898).
func TestAllOf_ARedeclarationIsJudgedOnceTheComponentsHaveLowered(t *testing.T) {
	t.Parallel()
	p, q := "{$ref: '#/components/schemas/P'}", "{$ref: '#/components/schemas/Q'}"
	for _, tc := range []struct {
		name, pdecl, qdecl string
		items              bool
		kind               ir.TypeKind
		code               string // the one diagnostic, or "" for none
	}{
		{"enums declared alike", "{type: string, enum: [a, b]}", "{type: string, enum: [a, b]}", false, ir.KindEnum, diag.DegradedConstruct},
		{"an integer beside a number", "{type: number}", "{type: integer}", false, ir.KindScalar, diag.DegradedConstruct},
		{"a string against an integer", "{type: string}", "{type: integer}", false, ir.KindScalar, diag.ConflictingRedecl},
		{"enums declared alike as list items", "{type: string, enum: [a, b]}", "{type: string, enum: [a, b]}", true, ir.KindList, diag.DegradedConstruct},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, b := p, q
			if tc.items {
				a, b = "{type: array, items: "+p+"}", "{type: array, items: "+q+"}"
			}
			pq := "    P: " + tc.pdecl + "\n    Q: " + tc.qdecl + "\n"
			qp := "    Q: " + tc.qdecl + "\n    P: " + tc.pdecl + "\n"
			for i, lower := range []func() (*ir.Document, []ir.Diagnostic){
				func() (*ir.Document, []ir.Diagnostic) { return schema.LowerSpec(t, nonBareSpec(pq, a, b)) },
				func() (*ir.Document, []ir.Diagnostic) { return settledSpec(t, pq, a, b) },
				func() (*ir.Document, []ir.Diagnostic) { return settledSpec(t, qp, a, b) },
			} {
				doc, diags := lower()
				m, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok, "order %d", i)
				require.Len(t, m.Properties, 1, "order %d", i)
				prop := m.Properties[0]

				assert.Equal(t, tc.kind, doc.Types[prop.Type.Target].Kind(), "order %d", i)
				if tc.code == "" {
					assert.Empty(t, diags, "order %d", i)
					assert.Empty(t, prop.Unmodeled, "order %d", i)
					continue
				}
				require.Len(t, diags, 1, "order %d", i)
				assert.Equal(t, tc.code, diags[0].Code, "order %d", i)
				assert.Len(t, prop.Unmodeled, 1, "the losing declaration is kept (order %d)", i)
			}
		})
	}
}

// TestAllOf_WhatAFieldKeepsOfItsDetailIsOrderFree compiles a field whose two
// declarations give an extension differently, alike in another, one promoting
// a deprecation from an extension, and one default spelled two ways. In either
// branch order the field keeps the first value of the extension and reports
// the other, names the heuristic behind its deprecation, and takes the default
// as the one value it is.
func TestAllOf_WhatAFieldKeepsOfItsDetailIsOrderFree(t *testing.T) {
	t.Parallel()
	a := "{type: number, default: 1, x-foo: 1, x-same: s}"
	b := "{type: number, default: 1.0, x-foo: 2, x-same: s, deprecated: true, x-deprecated-reason: use q}"
	for _, order := range [][2]string{{a, b}, {b, a}} {
		doc, diags := schema.LowerSpec(t, nonBareSpec("", order[0], order[1]))
		m, ok := doc.Types[componentID("T")].(*ir.Model)
		require.True(t, ok)
		require.Len(t, m.Properties, 1)
		p := m.Properties[0]
		first := "1"
		if order[0] == b {
			first = "2"
		}

		require.Contains(t, p.Unmodeled, "openapi:x-foo", "order %v", order)
		assert.Equal(t, ir.RawValue(first), p.Unmodeled["openapi:x-foo"].Value, "the first value stands")
		assert.Contains(t, p.Unmodeled, "openapi:x-same")
		assert.Contains(t, p.Unmodeled, "openapi:conflicting-redeclaration/components/schemas/T/allOf/1/properties/v",
			"the other declaration is kept verbatim")
		require.NotNil(t, p.Deprecation)
		assert.Equal(t, "use q", p.Deprecation.Message)
		assert.Equal(t, ir.Inferred("extension-promotion"), p.Provenance.Inferred, "the promoted deprecation stays marked")
		require.NotNil(t, p.Default)
		require.Len(t, diags, 1, "only the extension given differently is reported: %+v", diags)
		assert.Contains(t, diags[0].Message, "openapi:x-foo differently")
	}
}

// TestAllOf_ContainerMembersAreJudgedAsFieldsAre pins that two lists' members
// get the verdict two fields of those types get. Enums sharing no value, one
// list never empty, are a conflict; the same lists both admitting the empty one
// overlap in it. Members judged only as a fallback for top-level fields were
// once left unrelated, which the merge reported as a pair it does not compare.
func TestAllOf_ContainerMembersAreJudgedAsFieldsAre(t *testing.T) {
	t.Parallel()
	str, num := "{type: array, items: {type: string, enum: [a]}%s}", "{type: array, items: {type: integer, enum: [1]}}"
	for _, tc := range []struct {
		name, min string
		code      string
		sev       ir.Severity
	}{
		{"one list never empty", ", minItems: 1", diag.ConflictingRedecl, ir.SeverityWarning},
		{"both lists admitting the empty one", "", diag.DegradedConstruct, ir.SeverityInfo},
	} {
		for _, order := range [][2]string{{fmt.Sprintf(str, tc.min), num}, {num, fmt.Sprintf(str, tc.min)}} {
			_, diags := schema.LowerSpec(t, nonBareSpec("", order[0], order[1]))
			require.Len(t, diags, 1, "%s, order %v: %+v", tc.name, order, diags)
			assert.Equal(t, tc.code, diags[0].Code, "%s, order %v", tc.name, order)
			assert.Equal(t, tc.sev, diags[0].Severity, "%s, order %v", tc.name, order)
			if tc.code == diag.DegradedConstruct {
				assert.Contains(t, diags[0].Message, "whose values overlap", "%s, order %v", tc.name, order)
			}
		}
	}
}

// TestAllOf_PrimitiveNarrowingIsLosslessInEitherOrder pins that a redeclaration
// narrowing a bare primitive (GitHub #598, #446) loses nothing: the narrower
// target wins in both branch orders, with no diagnostic and no kept entry, and
// the two orders merge to the same type.
func TestAllOf_PrimitiveNarrowingIsLosslessInEitherOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, broad, narrow string
		want                ir.TypeID
	}{
		{"number and integer", "{type: number}", "{type: integer}", "t/prim/integer"},
		{"integer and int32", "{type: integer}", "{type: integer, format: int32}", "t/prim/int32"},
		{"string and date-time", "{type: string}", "{type: string, format: date-time}", "t/prim/datetime_offset"},
		{"int32 and int64", "{type: integer, format: int64}", "{type: integer, format: int32}", "t/prim/int32"},
		{"uri and string", "{type: string}", "{type: string, format: uri}", "t/prim/url"},
		{"double and number", "{type: number}", "{type: number, format: double}", "t/prim/float64"},
		{"uuid and string", "{type: string}", "{type: string, format: uuid}", "t/prim/uuid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var merged [2]ir.TypeRef
			for i, spec := range []string{allOfConflictSpec(tc.broad, tc.narrow), allOfConflictSpec(tc.narrow, tc.broad)} {
				doc, diags := schema.LowerSpec(t, spec)
				openapitest.RequireNoErrorDiags(t, diags)
				assert.Empty(t, diags, "a narrowing draws no diagnostic (order %d)", i)
				model, ok := doc.Types[componentID("T")].(*ir.Model)
				require.True(t, ok)
				require.Len(t, model.Properties, 1)
				prop := model.Properties[0]
				assert.Equal(t, tc.want, prop.Type.Target, "the narrower target wins (order %d)", i)
				for key := range prop.Unmodeled {
					assert.NotContains(t, key, "openapi:conflicting-redeclaration", "nothing is kept (order %d)", i)
				}
				merged[i] = prop.Type
			}
			assert.Empty(t, cmp.Diff(merged[0], merged[1]), "the merged type does not depend on branch order")
		})
	}
}

// nonBareSpec declares T with one property v written as schemaA in the first
// branch and schemaB in the second, after the components in extra.
func nonBareSpec(extra, schemaA, schemaB string) string {
	return openapitest.ComponentSpec(extra +
		"    T:\n" +
		"      allOf:\n" +
		"        - type: object\n" +
		"          properties: {v: " + schemaA + "}\n" +
		"        - type: object\n" +
		"          properties: {v: " + schemaB + "}\n")
}
