// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// capUseKeywords are what a use of one target writes beside its $ref and the
// election weighs nothing of: a note, what binds a use, a bound, a structural
// keyword and, where the dialect reads it so, the null the nullable-inference
// policy reads as widening the reference.
func capUseKeywords(version string) map[string]string {
	out := map[string]string{
		"none": "", "desc": "description: d", "ro": "readOnly: true", "dflt": "default: v0",
		"nullable": "nullable: true", "maxlen": "maxLength: 3", "props": "properties: {u: {type: string}}",
	}
	if version != "3.0.3" {
		out["typenull"] = "type: 'null'"
	}
	return out
}

// capUses writes each use of T, and of C, a $ref to T, in three spellings: the
// keywords beside the $ref, beside an allOf of it, and as a second entry. The
// null idiom is one beside a $ref alone, and elsewhere a conjunct of its own.
func capUses(version string) map[string]string {
	out := map[string]string{}
	for name, k := range capUseKeywords(version) {
		for _, x := range []string{"T", "C"} {
			ref := "$ref: '#/components/schemas/" + x + "'"
			beside := ""
			if k != "" {
				beside = ", " + k
			}
			out[name+"_"+x+"_ref"] = "{" + ref + beside + "}"
			if name == "typenull" {
				continue
			}
			if k != "" {
				out[name+"_"+x+"_entry"] = "{allOf: [{" + ref + "}, {" + k + "}]}"
			}
			out[name+"_"+x+"_allof"] = "{allOf: [{" + ref + "}]" + beside + "}"
		}
	}
	return out
}

// capUseSpec writes every use at a component, a property, a query parameter
// and a response, beside T sized n and C.
func capUseSpec(version, kind string, n int) (paths string, schemas []string) {
	uses := capUses(version)
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range uses {
			if !yield(name) {
				return
			}
		}
	})
	var p, props strings.Builder
	p.WriteString("paths:\n  /p:\n    get:\n      parameters:\n")
	for _, name := range names {
		fmt.Fprintf(&p, "        - {name: q_%s, in: query, schema: %s}\n", name, uses[name])
		fmt.Fprintf(&props, "p_%s: %s, ", name, uses[name])
		schemas = append(schemas, "U_"+name+": "+uses[name])
	}
	p.WriteString("      responses: {'204': {description: none}}\n")
	for _, name := range names {
		fmt.Fprintf(&p, "  /r/%s:\n    get:\n      responses: {'200': {description: ok, content: {application/json: {schema: %s}}}}\n",
			name, uses[name])
	}
	schemas = append(schemas, "H: {type: object, properties: {"+strings.TrimSuffix(props.String(), ", ")+"}}",
		"C: {$ref: '#/components/schemas/T'}")
	return p.String(), append(schemas, capTarget(kind, n)...)
}

// targetOwned matches what the target and its chain own, which grow with n.
var targetOwned = regexp.MustCompile(`/components/schemas/(T|C|S\d+)(/|$)`)

// useLowering is what the uses lower to, the target's own nodes and findings
// left out, the findings in one order whatever the declarations'.
type useLowering struct {
	types    map[ir.TypeID]ir.TypeDef
	services []ir.Service
	diags    []ir.Diagnostic
}

// useLoweringOf returns the part of doc and diags the uses own.
func useLoweringOf(doc *ir.Document, diags []ir.Diagnostic) useLowering {
	out := useLowering{types: map[ir.TypeID]ir.TypeDef{}, services: doc.Services}
	for id, td := range doc.Types {
		if !targetOwned.MatchString(string(id)) {
			out.types[id] = td
		}
	}
	for _, d := range diags {
		if !targetOwned.MatchString(string(d.Provenance.Pointer)) {
			out.diags = append(out.diags, d)
		}
	}
	slices.SortFunc(out.diags, func(a, b ir.Diagnostic) int {
		return cmp.Or(cmp.Compare(a.Provenance.Pointer, b.Provenance.Pointer), cmp.Compare(a.Code, b.Code),
			cmp.Compare(a.Message, b.Message))
	})
	return out
}

// TestCompile_AUseOfOneRefLowersAlikeWhateverItsTargetsSize holds a use of one
// target the election weighs alone, `{$ref: T, K}`, `{allOf: [{$ref: T}], K}`
// and `{allOf: [{$ref: T}, K]}` for K a note, a use's binding, a bound or a
// structural keyword, at a component, a property, a parameter and a response,
// over T and over a $ref to it, to what it lowers to within the caps of
// docs/compilers/openapi.md#allof-refinement, past them and in either order:
// the caps stop only what an election compares, and it compares nothing here.
func TestCompile_AUseOfOneRefLowersAlikeWhateverItsTargetsSize(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, tc := range []struct {
			kind  string
			sizes []int
		}{{"union", []int{100, 127, 128, 300}}, {"enum", []int{4096, 4097}}} {
			t.Run(version+" "+tc.kind, func(t *testing.T) {
				t.Parallel()
				var within *useLowering
				for _, n := range tc.sizes {
					paths, schemas := capUseSpec(version, tc.kind, n)
					inBothOrders(t, schemas, func(t *testing.T, schemas []string) {
						doc, diags := compileAnnotationSpec(t, "uses", admissionSpec(version, paths, schemas...))
						requireNoModelOverStrings(t, doc)
						got := useLoweringOf(doc, diags)
						if within == nil {
							within = &got
							return
						}
						assert.Empty(t, gocmp.Diff(*within, got, gocmp.AllowUnexported(useLowering{}),
							cmpopts.IgnoreTypes(ir.Position{})), "size %d lowers as size %d", n, tc.sizes[0])
					})
				}
			})
		}
	}
}

// TestCompile_AUseOfATargetAdmittingNoKindLowersAlikePastTheCaps holds a use
// of a target whose conjuncts share no kind to what it lowers to within the
// caps, where its read finds no value, past them, where its kinds alone say
// so: `{$ref: N, description: d}` stays the composition the conjuncts sharing
// no value leave, and `{allOf: [{$ref: N}]}`, saying nothing more, the
// reference (docs/compilers/openapi.md#allof-refinement).
func TestCompile_AUseOfATargetAdmittingNoKindLowersAlikePastTheCaps(t *testing.T) {
	t.Parallel()
	var within *useLowering
	// A use's read takes in 2n+5 schemas, so the caps hold up to 125 branches.
	for _, n := range []int{100, 125, 126, 300} {
		schemas := append([]string{"C: {allOf: [{$ref: '#/components/schemas/T'}, {type: integer}]}",
			"U_note: {$ref: '#/components/schemas/C', description: d}", "U_one: {allOf: [{$ref: '#/components/schemas/C'}]}"},
			capTarget("union", n)...)
		doc, diags := compileAnnotationSpec(t, "none", admissionSpec("3.1.0", "", schemas...))
		_, model := typeByName(doc, "U_note").(*ir.Model)
		assert.True(t, model, "size %d: the note's use composes", n)
		one, ok := typeByName(doc, "U_one").(*ir.Scalar)
		require.True(t, ok, "size %d: the use saying nothing more is the reference", n)
		assert.Equal(t, componentID("C"), one.Base.Target)
		got := useLoweringOf(doc, diags)
		if within == nil {
			within = &got
			continue
		}
		assert.Empty(t, gocmp.Diff(*within, got, gocmp.AllowUnexported(useLowering{}), cmpopts.IgnoreTypes(ir.Position{})))
	}
}

// requireNoModelOverStrings holds each use at a component to no Model: T
// admits strings alone, so a Model would assert an object the source rules out.
func requireNoModelOverStrings(t *testing.T, doc *ir.Document) {
	t.Helper()
	for id, td := range doc.Types {
		if strings.HasPrefix(string(id), "type/openapi/components/schemas/U_") {
			_, model := td.(*ir.Model)
			require.False(t, model, "%s lowers to a Model", id)
		}
	}
}
