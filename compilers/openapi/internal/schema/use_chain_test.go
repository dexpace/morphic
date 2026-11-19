// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// unloweredConstruct is the code a default that no ir.Value holds is reported
// under, spelled out so a failing run names it.
const unloweredConstruct = "openapi/unlowered-construct"

// chainComponents is a Leaf, two aliases of it that write their own default and
// description, an alias of an alias that writes nothing, and a model reading
// each, one component to an element in the order a spec could declare them.
func chainComponents() []string {
	return []string{
		"    Leaf: {type: string, description: from-leaf, default: l}\n",
		"    Fine: {$ref: '#/components/schemas/Leaf', description: from-fine, default: f}\n",
		"    Bare: {$ref: '#/components/schemas/Fine'}\n",
		"    Docs: {$ref: '#/components/schemas/Leaf', description: from-docs}\n",
		"    Holder:\n" +
			"      type: object\n" +
			"      properties:\n" +
			"        f: {$ref: '#/components/schemas/Fine'}\n" +
			"        b: {$ref: '#/components/schemas/Bare'}\n" +
			"        d: {$ref: '#/components/schemas/Docs'}\n" +
			"        own: {$ref: '#/components/schemas/Fine', description: from-use}\n",
	}
}

// chainWant is what each Holder property takes from its chain: the nearest hop
// writing a default and the nearest writing a description, each read on its own.
var chainWant = map[string]struct{ def, desc string }{
	"f":   {"f", "from-fine"},
	"b":   {"f", "from-fine"},
	"d":   {"l", "from-docs"},
	"own": {"f", "from-use"},
}

// TestUseOf_FallsBackToEachHopOfTheChain pins a use reading its $ref chain one hop at
// a time, nearest first: an alias's description and an alias's default are each
// the nearest hop writing that field, not the chain's end's, so a hop writing
// one and not the other lends the one and lets the other fall through to the
// next hop. A property's own words still win. Both declaration orders read the
// same, and the only findings are the two infos main already draws.
func TestUseOf_FallsBackToEachHopOfTheChain(t *testing.T) {
	t.Parallel()
	for name, reverse := range map[string]bool{"declared order": false, "reversed order": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lines := chainComponents()
			if reverse {
				slices.Reverse(lines)
			}
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", strings.Join(lines, "")))
			openapitest.RequireNoErrorDiags(t, diags)

			for wire, want := range chainWant {
				property := propertyOf(t, doc, "Holder", wire)
				require.NotNil(t, property.Default, wire)
				assert.Equal(t, ir.Value{Kind: ir.ValueString, String: want.def}, *property.Default, "%s: the nearest default", wire)
				assert.Equal(t, want.desc, property.Docs.Description, "%s: the nearest description", wire)
			}
			assert.Equal(t, []string{
				"info openapi/degraded-construct /components/schemas/Fine/default",
				"info openapi/degraded-construct /components/schemas/Leaf/default",
			}, findings(diags), "no finding but the two declarations' defaults kept verbatim")
		})
	}
}

// TestUseOf_FallsBackToEachHopAcrossTwoDocuments pins the same reading where the chain
// leaves the document: the alias and the leaf it names are written in another
// file, and the use's own alias in this one.
func TestUseOf_FallsBackToEachHopAcrossTwoDocuments(t *testing.T) {
	t.Parallel()
	const other = `components:
  schemas:
    Leaf: {type: string, description: from-leaf, default: l}
    Fine: {$ref: '#/components/schemas/Leaf', description: from-fine, default: f}
    Docs: {$ref: '#/components/schemas/Leaf', description: from-docs}
`
	const root = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Bare: {$ref: 'other.yaml#/components/schemas/Fine'}
    Holder:
      type: object
      properties:
        f: {$ref: 'other.yaml#/components/schemas/Fine'}
        b: {$ref: '#/components/schemas/Bare'}
        d: {$ref: 'other.yaml#/components/schemas/Docs'}
        own: {$ref: 'other.yaml#/components/schemas/Fine', description: from-use}
`
	doc, diags, _ := compileBeside(t, map[string]string{"other.yaml": other}, root)
	openapitest.RequireNoErrorDiags(t, diags)

	for wire, want := range chainWant {
		property := propertyOf(t, doc, "Holder", wire)
		require.NotNil(t, property.Default, wire)
		assert.Equal(t, ir.Value{Kind: ir.ValueString, String: want.def}, *property.Default, "%s: the nearest default", wire)
		assert.Equal(t, want.desc, property.Docs.Description, "%s: the nearest description", wire)
	}
}

// aliasChain returns n aliases, A0 to A(n-1), each naming the next, and the
// schema An they end on, which writes a default and a description. A hop at
// index writer, where there is one, writes a description beside its $ref too.
func aliasChain(n, writer int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "    A%d: {$ref: '#/components/schemas/A%d'", i, i+1)
		if i == writer {
			b.WriteString(", description: from-alias")
		}
		b.WriteString("}\n")
	}
	fmt.Fprintf(&b, "    A%d: {type: string, default: deep, description: from-end}\n", n)
	b.WriteString("    Holder: {type: object, properties: {p: {$ref: '#/components/schemas/A0'}}}\n")
	return b.String()
}

// TestUseOf_AnAliasHopIsNoReadThrough pins that a run of aliases is read within one
// turn: no length of it, whether one alias or well past the bound on read-throughs,
// costs the end's default or description. A hop in the middle of the run that
// writes a description is the nearest to write one, at every length.
func TestUseOf_AnAliasHopIsNoReadThrough(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2, 63, 64, 65, 70, 200} {
		t.Run(fmt.Sprintf("%d aliases", n), func(t *testing.T) {
			t.Parallel()
			bare, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", aliasChain(n, -1)))
			openapitest.RequireNoErrorDiags(t, diags)
			property := propertyOf(t, bare, "Holder", "p")
			require.NotNil(t, property.Default)
			assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "deep"}, *property.Default, "the end's default")
			assert.Equal(t, "from-end", property.Docs.Description, "the end's description")

			written, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", aliasChain(n, n/2)))
			openapitest.RequireNoErrorDiags(t, diags)
			property = propertyOf(t, written, "Holder", "p")
			require.NotNil(t, property.Default)
			assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "deep"}, *property.Default, "no alias writes a default")
			assert.Equal(t, "from-alias", property.Docs.Description, "the nearest hop that writes a description")
		})
	}
}

// TestUseOf_AnAliasHopIsNoReadThroughAcrossTwoDocuments is the same run of aliases
// cut in two: the first half is written here and the second in another file.
func TestUseOf_AnAliasHopIsNoReadThroughAcrossTwoDocuments(t *testing.T) {
	t.Parallel()
	for _, n := range []int{2, 65, 70} {
		t.Run(fmt.Sprintf("%d aliases", n), func(t *testing.T) {
			t.Parallel()
			half := n / 2
			var root, other strings.Builder
			root.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n")
			other.WriteString("components:\n  schemas:\n")
			for i := range half - 1 {
				fmt.Fprintf(&root, "    A%d: {$ref: '#/components/schemas/A%d'}\n", i, i+1)
			}
			fmt.Fprintf(&root, "    A%d: {$ref: 'other.yaml#/components/schemas/B0'}\n", half-1)
			for i := range n - half {
				next := fmt.Sprintf("B%d", i+1)
				if i == n-half-1 {
					next = "End"
				}
				fmt.Fprintf(&other, "    B%d: {$ref: '#/components/schemas/%s'", i, next)
				if i == (n-half)/2 {
					other.WriteString(", description: from-alias")
				}
				other.WriteString("}\n")
			}
			other.WriteString("    End: {type: string, default: deep, description: from-end}\n")
			root.WriteString("    Holder: {type: object, properties: {p: {$ref: '#/components/schemas/A0'}}}\n")

			doc, diags, _ := compileBeside(t, map[string]string{"other.yaml": other.String()}, root.String())
			openapitest.RequireNoErrorDiags(t, diags)
			property := propertyOf(t, doc, "Holder", "p")
			require.NotNil(t, property.Default)
			assert.Equal(t, ir.Value{Kind: ir.ValueString, String: "deep"}, *property.Default)
			assert.Equal(t, "from-alias", property.Docs.Description)
		})
	}
}

// readThroughChainSpec returns n schemas that each narrow the next by an enum written
// beside an allOf naming it, so a use passes over one $ref per link, and the
// last, whose description is what a use that reached it reads. Where aliases is
// above zero, that many aliases stand between each link and the next.
func readThroughChainSpec(n, aliases int) string {
	var b strings.Builder
	for i := range n {
		target := fmt.Sprintf("P%d", i+1)
		for k := range aliases {
			alias := fmt.Sprintf("X%d_%d", i, k)
			fmt.Fprintf(&b, "    %s: {$ref: '#/components/schemas/%s'}\n", alias, target)
			target = alias
		}
		fmt.Fprintf(&b, "    P%d: {enum: [a], allOf: [{$ref: '#/components/schemas/%s'}]}\n", i, target)
	}
	fmt.Fprintf(&b, "    P%d: {type: string, enum: [a], description: from-end}\n", n)
	b.WriteString("    Holder: {type: object, properties: {p: {$ref: '#/components/schemas/P0'}}}\n")
	return b.String()
}

// TestUseOf_ReadThroughsAreBoundedAndAliasesSpendNone pins the bound on the read-throughs
// one use reads on through, which an alias between two links does not move: a
// chain of links under it ends on the schema that describes it, and one at or
// past it stops short, with the same verdict whether or not aliases stand
// between the links.
func TestUseOf_ReadThroughsAreBoundedAndAliasesSpendNone(t *testing.T) {
	t.Parallel()
	const readThroughs = 64
	for _, aliases := range []int{0, 3} {
		for _, n := range []int{1, 2, readThroughs - 1, readThroughs, readThroughs + 1} {
			t.Run(fmt.Sprintf("%d links, %d aliases between", n, aliases), func(t *testing.T) {
				t.Parallel()
				doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", readThroughChainSpec(n, aliases)))
				openapitest.RequireNoErrorDiags(t, diags)
				want := ""
				if n < readThroughs {
					want = "from-end"
				}
				assert.Equal(t, want, propertyOf(t, doc, "Holder", "p").Docs.Description)
			})
		}
	}
}

// TestUseDefault_ReportsAtTheDefault pins where a default no ir.Value holds is
// reported: at that default's own pointer, once however many uses read it, and
// at no use. The declaration N keeps its own error for the verbatim copy it
// cannot make, at the same pointer; the example's report is its own.
func TestUseDefault_ReportsAtTheDefault(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a:
    get:
      parameters:
        - {name: q, in: query, schema: {type: number, default: .nan}}
      responses:
        '204': {description: ok}
components:
  schemas:
    N: {type: number, default: .inf}
    H:
      type: object
      properties:
        own: {type: number, default: .nan}
        viaRef: {$ref: '#/components/schemas/N'}
        alsoRef: {$ref: '#/components/schemas/N'}
        ex: {type: number, examples: [.nan]}
`)
	assert.Equal(t, []string{
		"warning openapi/unlowered-construct /components/schemas/H/properties/own/default",
		"warning openapi/unlowered-construct /components/schemas/N/default",
		"warning openapi/unlowered-construct /paths/~1a/get/parameters/0/schema/default",
	}, findingsOf(diags, unloweredConstruct), "one finding per default, at the default, a parameter's too")
	assert.Equal(t, []string{"error openapi/unpreservable-construct /components/schemas/N/default"},
		findingsOf(diags, diag.UnpreservableConstruct), "the verbatim copy N cannot keep is another loss")
	assert.Equal(t, 1, countCode(diags, diag.DegradedConstruct, "/components/schemas/H/properties/ex/examples/0"),
		"an example is reported at its own position")
	for _, wire := range []string{"own", "viaRef", "alsoRef"} {
		pointer := "/components/schemas/H/properties/" + wire
		assert.Zero(t, countCode(diags, diag.DegradedConstruct, pointer), "nothing at the carrier %s", wire)
		assert.Zero(t, countCode(diags, unloweredConstruct, pointer), "nothing at the carrier %s", wire)
		assert.Nil(t, propertyOf(t, doc, "H", wire).Default, wire)
	}
	assert.Nil(t, propertyOf(t, doc, "H", "ex").Default)
}

// TestUseDefault_ReportsAHeadersDefaultAtTheDefault is the same report for a
// response header, whose schema is its own and which reads another by $ref.
func TestUseDefault_ReportsAHeadersDefaultAtTheDefault(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a:
    get:
      responses:
        '200':
          description: ok
          headers:
            X-N: {schema: {$ref: '#/components/schemas/N'}}
            X-O: {schema: {type: number, default: .nan}}
components:
  schemas:
    N: {type: number, default: .inf}
`)
	assert.Equal(t, []string{
		"warning openapi/unlowered-construct /components/schemas/N/default",
		"warning openapi/unlowered-construct /paths/~1a/get/responses/200/headers/X-O/schema/default",
	}, findingsOf(diags, unloweredConstruct))
	op := openapitest.FirstOp(t, doc.Services[0])
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 2)
	for _, header := range op.Responses[0].Headers {
		assert.Nil(t, header.Default, header.WireName)
	}
}

// TestUseDefault_AChainReportsAtTheHopThatSupplied pins that the nearest default
// a chain writes is the one a use reads, and that when it is no value the IR
// holds the use takes none, rather than reading on to the next hop's: the
// finding is at the default that failed.
func TestUseDefault_AChainReportsAtTheHopThatSupplied(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", `    A: {$ref: '#/components/schemas/B', default: .nan}
    B: {type: number, default: 1}
    H:
      type: object
      properties:
        p: {$ref: '#/components/schemas/A'}
`))
	assert.Equal(t, []string{"warning openapi/unlowered-construct /components/schemas/A/default"},
		findingsOf(diags, unloweredConstruct))
	assert.Nil(t, propertyOf(t, doc, "H", "p").Default, "the hop that failed is the one that supplied it")
}

// findings returns each diagnostic in diags as "severity code pointer", sorted.
func findings(diags []ir.Diagnostic) []string {
	return findingsOf(diags, "")
}

// findingsOf returns the diagnostics of code, or every one where code is empty,
// as "severity code pointer", sorted.
func findingsOf(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, diagnostic := range diags {
		if code == "" || diagnostic.Code == code {
			out = append(out, fmt.Sprintf("%s %s %s", diagnostic.Severity, diagnostic.Code, diagnostic.Provenance.Pointer))
		}
	}
	slices.Sort(out)
	return out
}

// countCode counts the diagnostics of code at pointer.
func countCode(diags []ir.Diagnostic, code, pointer string) int {
	return len(diagsAtPointer(diags, code, pointer))
}

// TestUseDefault_ADeclarationsUnheldDefaultReachesNoCarrier holds the info a
// declaration's default draws to what its carriers take: one no ir.Value holds
// is kept and applied to no carrier, as the warning at the same default says,
// and one that converts reaches each referencing carrier.
func TestUseDefault_ADeclarationsUnheldDefaultReachesNoCarrier(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    K: {type: string, default: !acme x}
    G: {type: string, default: g}
    H:
      type: object
      properties:
        k: {$ref: '#/components/schemas/K'}
        g: {$ref: '#/components/schemas/G'}
`)
	for _, tc := range []struct{ name, reach string }{
		{"K", "applied to no carrier, as no IR value holds it"},
		{"G", "applied to a referencing property, header or parameter wherever that carrier has a field for it"},
	} {
		at := jsontext.Pointer("/components/schemas/" + tc.name + "/default")
		i := slices.IndexFunc(diags, func(diagnostic ir.Diagnostic) bool {
			return diagnostic.Code == diag.DegradedConstruct && diagnostic.Provenance.Pointer == at
		})
		require.GreaterOrEqual(t, i, 0, "%s keeps its default with an info", tc.name)
		assert.True(t, strings.HasSuffix(diags[i].Message, tc.reach), "%s: %s", tc.name, diags[i].Message)
	}
	assert.Nil(t, propertyOf(t, doc, "H", "k").Default)
	assert.NotNil(t, propertyOf(t, doc, "H", "g").Default)
}

// TestUseOf_AReferenceTheTableDidNotReadFallsBackToNothing pins a use of a chain of
// three aliases whose middle one, and a property beside it, write a $ref
// merged in past nodeview.MergeDepthLimit, which the parser's model holds and
// the table never read: each lands nowhere and lends nothing past it, so a
// property naming the chain takes its first hop's words and the other none,
// its own lying past the bound too, and each such $ref is reported
// unresolved.
func TestUseOf_AReferenceTheTableDidNotReadFallsBackToNothing(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: '1'}\npaths: {}\n"+
		"x-deep: &deep "+openapitest.NestedMerges(nodeview.MergeDepthLimit,
		"{$ref: '#/components/schemas/C', description: deep}")+"\n"+
		"components:\n  schemas:\n"+
		"    A: {$ref: '#/components/schemas/B', description: a}\n"+
		"    B: {<<: *deep}\n"+
		"    C: {type: string, description: c, deprecated: true}\n"+
		"    Holder: {type: object, properties: {p: {$ref: '#/components/schemas/A'}, q: {<<: *deep}}}\n")
	property := propertyOf(t, doc, "Holder", "p")
	assert.Equal(t, componentID("A"), property.Type.Target)
	assert.Equal(t, "a", property.Docs.Description, "the first hop's words")
	assert.Nil(t, property.Deprecation, "nothing past the unread $ref is lent")
	irProperty := propertyOf(t, doc, "Holder", "q")
	assert.Equal(t, ir.TypeID("type/primitive/any"), irProperty.Type.Target)
	assert.Empty(t, irProperty.Docs.Description, "its words lie past the merge bound, which are not read")
	assert.Nil(t, irProperty.Deprecation)
	assert.Len(t, withCode(diags, diag.UnresolvedRef), 2, "B's and q's")
}
