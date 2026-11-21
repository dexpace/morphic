// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// nullCase is one way a schema says what it does of null, and whether it
// admits null.
type nullCase struct {
	name, schema string
	null         bool
}

// deadBranchBodies are bodies a union is distributed over, admitting objects
// and, or not, null: by a type list or 3.0's nullable, by an allOf entry
// admitting it, by neither, and by neither beside an allOf.
func deadBranchBodies(version string) []nullCase {
	nullable := "type: [object, 'null']"
	if version == "3.0.3" {
		nullable = "type: object, nullable: true"
	}
	return []nullCase{
		{"own", nullable, true},
		{"allOf", "allOf: [" + capRef("NO") + "]", true},
		{"none", "type: object", false},
		{"none beside an allOf", "type: object, allOf: [" + capRef("OO") + "]", false},
	}
}

// deadBranchSources are the unions T a branch names, of a string and a second
// branch admitting null, or not, in each way it can: inline and null alone, a
// $ref to a null schema, a type list, 3.0's nullable, an enum of null alone, a
// further union, an allOf, a chain of $refs, a $ref beside a description and,
// at 3.1, a const.
func deadBranchSources(version, keyword string) []nullCase {
	union := func(second string) string { return "{" + keyword + ": [" + capRef("S0") + ", " + second + "]}" }
	inline, typeList := "{type: 'null'}", "{type: [integer, 'null']}"
	if version == "3.0.3" {
		inline, typeList = "{nullable: true, enum: [null]}", "{type: integer, nullable: true}"
	}
	out := []nullCase{
		{"inline", union(inline), true},
		{"ref", union(capRef("NUL")), true},
		{"type list", union(typeList), true},
		{"nullable", union(capRef("NS")), true},
		{"enum", union("{enum: [null]}"), true},
		{"union", union(capRef("U2")), true},
		{"allOf", union("{allOf: [" + capRef("NUL") + "]}"), true},
		{"chain", union(capRef("C1")), true},
		{"description", union("{$ref: '#/components/schemas/NUL', description: d}"), true},
		{"none", union(capRef("S1")), false},
	}
	if version != "3.0.3" {
		out = append(out, nullCase{"const", union("{const: null}"), true})
	}
	return out
}

// deadBranchSpec writes P and Pr, body distributing a union of branch and O
// in either order, beside what branch and T name and a property naming T.
func deadBranchSpec(version, keyword, body, t, branch string) string {
	null, ns, no := "{type: 'null'}", "{type: [string, 'null']}", "{type: [object, 'null']}"
	if version == "3.0.3" {
		null, ns, no = "{type: string, nullable: true, enum: [null]}", "{type: string, nullable: true}",
			"{type: object, nullable: true}"
	}
	return admissionSpec(version, "",
		"S0: {type: string}", "S1: {type: string, format: email}", "NUL: "+null, "NS: "+ns, "NO: "+no,
		"OO: {type: object}", "C1: "+capRef("NUL"), "U2: {"+keyword+": ["+capRef("S1")+", "+capRef("NUL")+"]}",
		"O: {type: object, properties: {o: {type: string}}}", "T: "+t,
		"P: {"+body+", properties: {u: {type: string}}, "+keyword+": ["+branch+", "+capRef("O")+"]}",
		"Pr: {"+body+", properties: {u: {type: string}}, "+keyword+": ["+capRef("O")+", "+branch+"]}",
		"H: {type: object, properties: {t: "+capRef("T")+"}}")
}

// TestCompile_ABranchIsDeadWhereItOrItsBodyAdmitsNoNull holds the branch of a
// union distributed over an object body, admitting no object, to the rule of
// docs/compilers/openapi.md#union-beside-structure: it is dead exactly where it
// or the body admits no null, its null read wherever the IR holds it, on a
// variant of the union it names as on the reference to it, and the body's by
// all it writes but the union. The warning says the branch admits no null
// only where it admits none, and the body admits null aside only where it
// may; in either order, at 3.0 and 3.1, through a $ref and inline.
func TestCompile_ABranchIsDeadWhereItOrItsBodyAdmitsNoNull(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, keyword := range []string{"anyOf", "oneOf"} {
			for _, body := range deadBranchBodies(version) {
				for _, source := range deadBranchSources(version, keyword) {
					for _, inline := range []bool{false, true} {
						branch := capRef("T")
						if inline {
							branch = source.schema
						}
						name := strings.Join([]string{version, keyword, body.name, source.name}, " ")
						if inline {
							name += " inline"
						}
						t.Run(name, func(t *testing.T) {
							t.Parallel()
							doc, diags := compileAnnotationSpec(t, "dead",
								deadBranchSpec(version, keyword, body.schema, source.schema, branch))
							requireDeadAsTheNullsSay(t, doc, diags, keyword, !source.null || !body.null, body.null)
							assert.Equal(t, source.null, refOrVariantAdmitsNull(doc), "the IR holds T's null")
						})
					}
				}
			}
		}
	}
}

// requireDeadAsTheNullsSay holds P and Pr's branch over T dead or living as
// dead says, and the warning a dead one draws to what the body admits.
func requireDeadAsTheNullsSay(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, keyword string, dead, bodyNull bool) {
	t.Helper()
	for name, at := range map[string]string{"P": keyword + "/0", "Pr": keyword + "/1"} {
		union, ok := typeByName(doc, name).(*ir.Union)
		require.True(t, ok, "%s: %T", name, typeByName(doc, name))
		assert.Equal(t, dead, union.Unmodeled["openapi:"+at].Value != nil, "%s: the branch over T is dead", name)
		var warning []string
		for _, diagnostic := range diags {
			if diagnostic.Code == diag.DegradedConstruct && diagnostic.Severity == ir.SeverityWarning &&
				string(diagnostic.Provenance.Pointer) == "/components/schemas/"+name {
				warning = append(warning, diagnostic.Message)
			}
		}
		if !dead {
			assert.Empty(t, warning, name)
			continue
		}
		require.Len(t, warning, 1, name)
		said := "admits objects alone, and 1 of its branches admits no object ("
		if bodyNull {
			said = "admits objects alone, null aside, and 1 of its branches admits no object and no null ("
		}
		assert.Contains(t, warning[0], said, name)
	}
}

// TestCompile_ABodyWhoseNullTheWalkCannotFinishMayAdmitIt holds the bound of
// the null reading beside a union (docs/compilers/openapi.md#union-beside-structure):
// a body whose allOf chain the walk reads to its end keeps out the null its
// end keeps out, so a branch over T, which admits null, is dead; one the walk
// cannot finish may admit null, so the branch lives; and one whose own type
// keeps null out does so however deep its chain.
func TestCompile_ABodyWhoseNullTheWalkCannotFinishMayAdmitIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		own   string
		depth int
		dead  bool
	}{{"", 127, true}, {"", 128, false}, {"", 300, false}, {"type: object, ", 300, true}} {
		t.Run(fmt.Sprintf("%sdepth %d", tc.own, tc.depth), func(t *testing.T) {
			t.Parallel()
			schemas := []string{"S0: {type: string}", "NUL: {type: 'null'}", "O: {type: object, properties: {o: {type: string}}}",
				"T: {anyOf: [" + capRef("S0") + ", " + capRef("NUL") + "]}",
				"P: {" + tc.own + "allOf: [" + capRef("B0") + "], properties: {u: {type: string}}, anyOf: [" +
					capRef("T") + ", " + capRef("O") + "]}"}
			end := "{type: object}"
			if tc.own != "" {
				end = "{type: [object, 'null']}"
			}
			for i := range tc.depth - 1 {
				schemas = append(schemas, fmt.Sprintf("B%d: {allOf: [%s]}", i, capRef(fmt.Sprintf("B%d", i+1))))
			}
			schemas = append(schemas, fmt.Sprintf("B%d: %s", tc.depth-1, end))
			doc, diags := compileAnnotationSpec(t, "deep", admissionSpec("3.1.0", "", schemas...))
			union, ok := typeByName(doc, "P").(*ir.Union)
			require.True(t, ok, "%T", typeByName(doc, "P"))
			assert.Equal(t, tc.dead, union.Unmodeled["openapi:anyOf/0"].Value != nil, "depth %d: the branch over T is dead", tc.depth)
			assert.Equal(t, tc.dead, countFindings(diags, diag.DegradedConstruct, ir.SeverityWarning, "/components/schemas/P") == 1)
		})
	}
}

// refOrVariantAdmitsNull reports whether the IR says T admits null: the
// reference H.t holds is nullable, or a variant of T's union is, or names a
// union one of whose variants is.
func refOrVariantAdmitsNull(doc *ir.Document) bool {
	model, ok := typeByName(doc, "H").(*ir.Model)
	if !ok || len(model.Properties) != 1 {
		return false
	}
	ref := model.Properties[0].Type
	return ref.Nullable || variantAdmitsNull(doc, ref.Target, 4)
}

// variantAdmitsNull reports whether a variant of the union id names admits
// null, nested no deeper than depth.
func variantAdmitsNull(doc *ir.Document, id ir.TypeID, depth int) bool {
	union, ok := doc.Types[id].(*ir.Union)
	if !ok || depth == 0 {
		return false
	}
	for _, variant := range union.Variants {
		if variant.Type.Nullable || variantAdmitsNull(doc, variant.Type.Target, depth-1) {
			return true
		}
	}
	return false
}
