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

// TestCompile_AVariantIsReadWithEverySchemaHoldingItsUnion holds that a field
// declared as a list, a nullable list or a string beside one whose union of an
// object, an array and a string sits in an allOf entry of an object, one or
// two entries deep, beside a typed entry, under a null or beside a null branch,
// is judged against what the object admits: no variant is an array or a
// string, so the merge does not compare the two and keeps the later with its
// own null, in both entry and component orders, at 3.0 and 3.1. The holder
// warns of its dead branches.
func TestCompile_AVariantIsReadWithEverySchemaHoldingItsUnion(t *testing.T) {
	t.Parallel()
	const union = "{anyOf: [{type: object}, {type: array, items: {}}, {type: string}]}"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		nullObject, nullList := "type: [object, 'null']", "{type: [array, 'null'], items: {}}"
		if version == "3.0.3" {
			nullObject, nullList = "type: object, nullable: true", "{type: array, items: {}, nullable: true}"
		}
		holders := map[string]struct {
			schema   string
			nullable bool
		}{
			"in an entry":          {"{type: object, allOf: [" + union + "]}", false},
			"beside a typed entry": {"{allOf: [{type: object}, " + union + "]}", false},
			"two entries deep":     {"{type: object, allOf: [{allOf: [" + union + "]}]}", false},
			"under a null":         {"{" + nullObject + ", allOf: [" + union + "]}", true},
		}
		if version == "3.1.0" {
			holders["beside a null branch"] = struct {
				schema   string
				nullable bool
			}{"{type: [object, 'null'], allOf: [{anyOf: [{type: object}, {type: array, items: {}}, {type: string}, {type: 'null'}]}]}", true}
		}
		seconds := map[string]struct {
			schema, target string
			nullable       bool
		}{
			"list":          {"{type: array, items: {}}", "", false},
			"nullable list": {nullList, "", true},
			"string":        {"{type: string}", "type/primitive/string", false},
		}
		for holderName, holder := range holders {
			for secondName, second := range seconds {
				schemas := []string{
					"Common: {type: object, properties: {r: " + holder.schema + "}}",
					"Coll1: {allOf: [{$ref: '#/components/schemas/Common'}, {properties: {r: " + second.schema + "}}]}",
					"Coll2: {allOf: [{properties: {r: " + second.schema + "}}, {$ref: '#/components/schemas/Common'}]}",
				}
				t.Run(fmt.Sprintf("%s/%s/%s", version, holderName, secondName), func(t *testing.T) {
					t.Parallel()
					inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
						doc, diags := compileAnnotationSpec(t, "holders", admissionSpec(version, "", schemas...))
						requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct,
							pointer: "/components/schemas/Common/properties/r", contains: []string{"/anyOf/1: array", "/anyOf/2: string"}}})
						var uncompared []string
						for _, d := range diags {
							if d.Code == diag.DegradedConstruct && strings.Contains(d.Message, "does not compare") {
								uncompared = append(uncompared, string(d.Provenance.Pointer))
							}
						}
						assert.ElementsMatch(t, []string{"/components/schemas/Coll1/allOf/0", "/components/schemas/Coll2/allOf/0/properties/r"},
							uncompared, "each order reports the two types the merge does not compare")
						target := second.target
						if target == "" {
							target = "type/anonymous/components/schemas/Coll1/allOf/1/properties/r"
						}
						assert.Equal(t, ir.TypeRef{Target: ir.TypeID(target), Nullable: second.nullable},
							typeOfR(t, doc, "Coll1"), "Coll1 keeps its later declaration, with its null")
						assert.Equal(t, ir.TypeRef{Target: "type/anonymous/components/schemas/Common/properties/r", Nullable: holder.nullable},
							typeOfR(t, doc, "Coll2"), "Coll2 keeps Common's union, with its null")
					})
				})
			}
		}
	}
}

// typeOfR is the type the property r of the component model gives.
func typeOfR(t *testing.T, doc *ir.Document, model string) ir.TypeRef {
	t.Helper()
	m, ok := doc.Types[ir.TypeID("type/openapi/components/schemas/"+model)].(*ir.Model)
	require.True(t, ok, "%s lowers to a model", model)
	for _, p := range m.Properties {
		if p.WireName == "r" {
			return p.Type
		}
	}
	require.Failf(t, "no such property", "%s has no property r", model)
	return ir.TypeRef{}
}
