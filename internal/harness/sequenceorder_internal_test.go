// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// sequenceVerdict is the sequence-order oracle's verdict in a result, failing
// the test where the result holds none.
func sequenceVerdict(t *testing.T, result Result) Verdict {
	t.Helper()
	for _, verdict := range result.Verdicts {
		if verdict.Oracle == OracleSequenceOrder {
			return verdict
		}
	}
	require.FailNow(t, "the result holds no verdict of the sequence-order oracle")
	return Verdict{}
}

// TestCheck_SequenceOrderOracleHoldsOnTheRealCompiler asks the oracle of
// sources the compiler lowers alike in either order of their sequences. Each
// row also holds what the compile reports and keeps, so the row's source
// reaches the construct its name gives it rather than a compile that happens
// to pass the oracle. Rows planting a defect in the oracle's reading of that
// construct go red on it.
func TestCheck_SequenceOrderOracleHoldsOnTheRealCompiler(t *testing.T) {
	const s = "/components/schemas"
	composes := func(name string, entries ...string) string {
		refs := make([]string, len(entries))
		for i, e := range entries {
			refs[i] = "{$ref: '#/components/schemas/" + e + "'}"
		}
		return name + ": {allOf: [" + strings.Join(refs, ", ") + "]}"
	}
	common := "Common: {type: object, properties: {success: {type: boolean, enum: [true]}}}"
	failure := "Failure: {type: object, properties: {success: {type: boolean, enum: [false]}}}"
	const merged = "the same holds at each of 2 models composing both"
	tests := []struct {
		name  string
		src   string
		kept  []string // substrings of the compiled document's JSON
		codes map[string]string
		want  Verdict
	}{
		{
			// A refined enum: the two declarations overlap and the merge keeps
			// one, which the report ties to the order.
			name: "two enums refining one property",
			src: schemas("AB: {allOf: [{properties: {p: {type: string, enum: [a, b, c]}}}, " +
				"{properties: {p: {type: string, enum: [c, b, d]}}}]}"),
			codes: map[string]string{"openapi/degraded-construct": s + "/AB/allOf/0/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			name: "a redeclared property whose types disagree",
			src: schemas("AB: {allOf: [{properties: {p: {type: string}, q: {type: object, properties: {x: {type: string}}}}}, " +
				"{properties: {p: {type: integer}, q: {type: object, properties: {y: {type: string}}}}}]}"),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/AB/allOf/0/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// The base's tie flips its member with the order, and the member a
			// model composing it declares again folds with what the tie kept.
			name: "a tie among inline declarations decides what overrides it",
			src: schemas("Base: {allOf: [{type: object, properties: {x: {type: integer}}}, {type: object, properties: {x: {type: string}}}]}",
				"Sub: {allOf: [{$ref: '#/components/schemas/Base'}, {type: object, properties: {x: {type: string, description: d}}}]}"),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/Base/allOf/0/properties/x"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// The models composing both components draw one report, at the
			// declaration that lost, which a reversal moves to the other
			// declaration rather than to another entry.
			name:  "a tie between two components several models compose",
			src:   schemas(composes("Both", "Common", "Failure"), composes("Both2", "Common", "Failure"), common, failure),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/Common/properties/success"},
			want:  passed(OracleSequenceOrder),
		},
		{
			name:  "the same tie with the components composed in the other order",
			src:   schemas(composes("Both", "Failure", "Common"), composes("Both2", "Failure", "Common"), common, failure),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/Failure/properties/success"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// Two declarations lose to the third, and the reversal makes the
			// first win: one report stays where it was, one moves.
			name: "two of three declarations losing to the last",
			src: schemas(composes("Both", "A", "B", "C"), composes("Both2", "A", "B", "C"),
				"A: {type: object, properties: {p: {type: string}}}", "B: {type: object, properties: {p: {type: integer}}}",
				"C: {type: object, properties: {p: {type: boolean}}}"),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/A/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			name: "a tie through aliases of the components and of the members' types",
			src: schemas(composes("Both", "CommonAlias", "FailureAlias"), composes("Both2", "CommonAlias", "FailureAlias"),
				"CommonAlias: {$ref: '#/components/schemas/Common'}", "FailureAlias: {$ref: '#/components/schemas/Failure'}",
				"Common: {type: object, properties: {success: {$ref: '#/components/schemas/True'}}}",
				"Failure: {type: object, properties: {success: {$ref: '#/components/schemas/False'}}}",
				"True: {type: boolean, enum: [true]}", "False: {type: boolean, enum: [false]}"),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/Common/properties/success"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// FailureX settles its own tie, so the member it brings keeps
			// Common's identity, and the report of the tie above it sits where
			// its type is written.
			name: "a tie at a member a composed component settles",
			src: schemas(composes("Both", "FailureX", "Other"), composes("Both2", "FailureX", "Other"), common,
				"FailureX: {allOf: [{$ref: '#/components/schemas/Common'}, {type: object, properties: {success: {type: string}}}]}",
				"Other: {type: object, properties: {success: {type: integer}}}"),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/FailureX/allOf/1/properties/success"},
			want:  passed(OracleSequenceOrder),
		},
		{
			name: "a tie three models compose, one of them with a member of its own",
			src: schemas(composes("Both", "Common", "Failure"), composes("Both2", "Common", "Failure"),
				"Both3: {allOf: [{$ref: '#/components/schemas/Common'}, {$ref: '#/components/schemas/Failure'}, "+
					"{type: object, properties: {extra: {type: string}}}]}",
				"Holder: {type: object, properties: {b: {$ref: '#/components/schemas/Both'}}}", common, failure),
			kept:  []string{"the same holds at each of 3 models composing both"},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/Common/properties/success"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// Each tie pairs with the report naming its own member: no model
			// folds success and code together.
			name: "ties on two members of one pair of declarations",
			src: schemas(composes("Both", "Common", "Failure"), composes("Both2", "Common", "Failure"),
				"Common: {type: object, properties: {success: {type: boolean, enum: [true]}, code: {type: integer}, same: {type: string}}}",
				"Failure: {type: object, properties: {success: {type: boolean, enum: [false]}, code: {type: string}, same: {type: string}}}"),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/Common/properties/code"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// The AB models' tie reports at A as written and at B reversed, and
			// the BA models', composing the other order, the other way round, so
			// each declaration holds one report in either order: the documents
			// show the two ties, in every member folding both.
			name: "two ties whose reports trade declarations",
			src: schemas(composes("AB1", "A", "B"), composes("AB2", "A", "B"), composes("BA1", "B", "A"), composes("BA2", "B", "A"),
				"A: {type: object, properties: {p: {type: string}}}", "B: {type: object, properties: {p: {type: integer}}}"),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/B/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// The same with the BA models' tie said for three: the members
			// take the same shapes in either order, in models of another
			// number.
			name: "two ties whose reports trade declarations, one said for more models",
			src: schemas(composes("AB1", "A", "B"), composes("AB2", "A", "B"), composes("BA1", "B", "A"), composes("BA2", "B", "A"),
				composes("BA3", "B", "A"), "A: {type: object, properties: {p: {type: string}}}",
				"B: {type: object, properties: {p: {type: integer}}}"),
			kept:  []string{"the same holds at each of 3 models composing both"},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/A/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// The AB models' tie moves its report from A to B, where the BC
			// models' sits as written before it moves to C.
			name: "a chain of ties sharing a declaration",
			src: schemas(composes("AB1", "A", "B"), composes("AB2", "A", "B"), composes("BC1", "B", "C"), composes("BC2", "B", "C"),
				"A: {type: object, properties: {p: {type: string}}}", "B: {type: object, properties: {p: {type: integer}}}",
				"C: {type: object, properties: {p: {type: boolean}}}"),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/B/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// As written, the ABC models' fold of three reports at A and at B,
			// and the AB models' at A again; reversed, both families report at B
			// alike, which the compiler says once for the four models, so the
			// two orders make three reports and two.
			name: "two families of models over shared components",
			src: schemas(composes("AB1", "A", "B"), composes("AB2", "A", "B"), composes("ABC1", "A", "B", "C"), composes("ABC2", "A", "B", "C"),
				"A: {type: object, properties: {p: {type: string}}}", "B: {type: object, properties: {p: {type: integer}}}",
				"C: {type: object, properties: {p: {type: boolean}}}"),
			kept:  []string{merged},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/B/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// M1's tie keeps B over A as written and A over B reversed, and M2,
			// whose A and C agree, keeps them over B in either order: reversed,
			// the two models say one conflict at B, once for both, and as
			// written each says its own, naming its member.
			name: "a conflict said for one model in one order and for two in the other",
			src: schemas(composes("M1", "A", "B"), composes("M2", "A", "B", "C"),
				"A: {type: object, properties: {p: {type: string}}}", "B: {type: object, properties: {p: {type: integer}}}",
				"C: {type: object, properties: {p: {type: string}}}"),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/A/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// The two families trade declarations, and M2 also requires p,
			// which its model decides whatever declaration p takes.
			name: "two ties trading declarations, one model requiring the member",
			src: schemas(composes("M1", "A", "B"), "M2: {allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}], required: [p]}",
				composes("N1", "B", "A"), composes("N2", "B", "A"),
				"A: {type: object, properties: {p: {type: string}}}", "B: {type: object, properties: {p: {type: integer}}}"),
			kept:  []string{merged, `"required":true`},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/B/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// The same trade where A gives a description, which each member
			// keeps whichever type it takes: no member takes what one
			// declaration says alone, and M2's requires p besides.
			name:  "two ties trading declarations, one giving a description both keep",
			src:   fixtureDescTrade,
			kept:  []string{merged, `"description":"da"`, `"required":true`},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/A/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// C2 loses to the booleans in either order, and which boolean its
			// report names as kept, and so which models say it together,
			// follows the order; no member differs.
			name: "a conflict regrouped at a declaration that loses in either order",
			src: schemas("M0: {allOf: [{type: object, properties: {p: {type: boolean}}}, {allOf: [{$ref: '#/components/schemas/C2'}]}, "+
				"{allOf: [{$ref: '#/components/schemas/C1'}]}]}",
				"M4: {allOf: [{allOf: [{$ref: '#/components/schemas/C2'}]}, {$ref: '#/components/schemas/C3'}, {allOf: [{$ref: '#/components/schemas/C1'}]}]}",
				"C1: {type: object, properties: {p: {type: boolean}}}", "C2: {type: object, properties: {p: {type: object, properties: {x: {type: string}}}}}",
				"C3: {type: object, properties: {p: {type: boolean}}}"),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/C2/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// As written C0 loses in M1 and C1 in M3 and M4; reversed C1 loses
			// in all four: the report at C1 is in both orders, and C0's has its
			// counterpart there.
			name: "a conflict at one declaration in one order and at another of its class in both",
			src: schemas(composes("M1", "C0", "C1"), "M3: {allOf: [{allOf: [{$ref: '#/components/schemas/C1'}]}, {$ref: '#/components/schemas/C0'}, "+
				"{$ref: '#/components/schemas/C1'}]}", composes("M4", "C1", "ViaC0", "C1"), composes("M5", "C0", "WrapC1"),
				"ViaC0: {$ref: '#/components/schemas/C0'}", "WrapC1: {allOf: [{$ref: '#/components/schemas/C1'}]}",
				"C0: {type: object, properties: {p: {type: integer}}}", "C1: {type: object, properties: {p: {type: string}}}"),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/C0/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			name: "a redeclared array keeps the declaration naming an item type",
			src: schemas("N: {allOf: [{type: object, properties: {p: {type: array}}}, " +
				"{type: object, properties: {p: {type: array, items: {type: string}}}}]}"),
			want: passed(OracleSequenceOrder),
		},
		{
			// Two inline copies of one node, each owning the type of its own
			// position: the types are told apart by what they say.
			name: "identical inline declarations in two entries",
			src: schemas("Copies: {allOf: [{type: object, properties: {owner: {type: object, properties: {name: {type: string}}}, a: {type: string}}}, " +
				"{type: object, properties: {owner: {type: object, properties: {name: {type: string}}}, b: {type: string}}}]}"),
			want: passed(OracleSequenceOrder),
		},
		{
			name: "mixins in either order",
			src: schemas("A: {type: object, properties: {x: {type: string}}}",
				"B: {type: object, properties: {y: {type: string}}}",
				"AB: {allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}"),
			kept: []string{`"mixins"`},
			want: passed(OracleSequenceOrder),
		},
		{
			name: "requires in either order",
			src: schemas("A: {type: object, properties: {x: {type: string}, y: {type: string}}}",
				"Sub: {allOf: [{$ref: '#/components/schemas/A'}, {type: object, required: [y, x]}]}"),
			kept: []string{`"requires":["y","x"]`},
			want: passed(OracleSequenceOrder),
		},
		{
			name: "a required array kept verbatim on a type with no home for it",
			src:  schemas("Str: {type: string, required: [b, a]}"),
			kept: []string{`"openapi:required"`, `"value":["b","a"]`},
			want: passed(OracleSequenceOrder),
		},
		{
			name: "an allOf array inside a verbatim keyword",
			src:  schemas("V: {type: object, not: {allOf: [{required: [a]}, {required: [b]}]}}"),
			kept: []string{`"openapi:not"`},
			want: passed(OracleSequenceOrder),
		},
		{
			// The kept entry is named by its index, which reversing moves.
			name:  "a false entry kept under a name carrying its index",
			src:   schemas("Never: {allOf: [{type: object, properties: {a: {type: string}}}, false]}"),
			kept:  []string{`"openapi:allOf/1"`},
			codes: map[string]string{"openapi/false-schema": s + "/Never/allOf/1"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// Two members whose names are one once their indices are erased.
			name: "two entries kept under names that differ by index alone",
			src:  schemas("Inline: {allOf: [{type: string}, {type: integer}]}"),
			kept: []string{`"openapi:allOf/0"`, `"openapi:allOf/1"`},
			want: passed(OracleSequenceOrder),
		},
		{
			// A reference names the first entry, so the sequence is left as it
			// is and the oracle has nothing to swap.
			name: "a sequence a reference points into is not permuted",
			src: schemas("N: {allOf: [{type: object, properties: {p: {type: number}}}, {type: object, properties: {p: {type: integer}}}]}",
				"User: {type: object, properties: {branch: {$ref: '#/components/schemas/N/allOf/0'}}}"),
			want: declined(OracleSequenceOrder, DeclineNothingToPermute),
		},
		{
			// The second document writes AB's allOf too, which the compiler
			// drops: reversing either would let the oracle read one as the
			// other.
			name: "a path two documents of the stream write",
			src: schemas("AB: {allOf: [{type: object, properties: {p: {type: string}}}, {type: object, properties: {p: {type: integer}}}]}") +
				"---\n" + schemas("AB: {allOf: [{type: object, properties: {a: {type: string}}}, "+
				"{type: object, properties: {b: {type: string}}}, {type: object, properties: {c: {type: string}}}]}"),
			codes: map[string]string{"openapi/stream-documents-dropped": ""},
			want:  declined(OracleSequenceOrder, DeclineNothingToPermute),
		},
		{
			// The lists win the tie as written, so the fold mints a list of a
			// model composing X and Y, which settles q and reports its own
			// conflict, in that order alone.
			name: "a tie a list of a composed model wins as written",
			src:  schemas(composes("M", "I1", "I2", "LX", "LY"), listsOfModels("{type: string}", "{type: integer}")),
			kept: []string{"p/items/allOf/allOf/properties/q"},
			codes: map[string]string{
				"openapi/conflicting-redeclaration": s + "/X/properties/q",
			},
			want: passed(OracleSequenceOrder),
		},
		{
			name:  "the same tie the lists win reversed",
			src:   schemas(composes("M", "LX", "LY", "I1", "I2"), listsOfModels("{type: string}", "{type: integer}")),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/LX/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// q's declarations meet, so the composed model's fold mints a
			// list of its own beneath the tied member.
			name: "a tie whose composed model mints for its own member",
			src: schemas(composes("M", "I1", "I2", "LX", "LY"),
				listsOfModels("{type: array, items: {type: string, description: d}}", "{type: array, items: {type: string, maxLength: 3}}")),
			kept: []string{`"type/composed/components/schemas/M/allOf/properties/p/items/allOf/allOf/properties/q"`},
			want: passed(OracleSequenceOrder),
		},
		{
			// D0's and D1's lists meet, each holding as many declarations, so the
			// later wins and D2's report keeps it: D1 as written, D0 reversed.
			// The member takes the list the fold mints in either order.
			name: "a conflict kept by whichever of two meeting lists is written later",
			src: schemas(composes("M", "D0", "D1", "D2"),
				"D0: {type: object, properties: {p: {type: array, minItems: 1, items: {type: string}}}}",
				"D1: {type: object, properties: {p: {type: array, maxItems: 5, items: {type: string}}}}",
				"D2: {type: object, properties: {p: {type: integer}}}"),
			kept:  []string{`"target":"type/composed/components/schemas/M/allOf/properties/p"`},
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/D2/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			name: "the same with two enums meeting in the values they share",
			src: schemas(composes("M", "D0", "D1", "D2"),
				"D0: {type: object, properties: {p: {type: string, enum: [a, b, c]}}}",
				"D1: {type: object, properties: {p: {type: string, enum: [b, c, d]}}}",
				"D2: {type: object, properties: {p: {type: integer}}}"),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/D2/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			name: "the same with a described list and a list of bounded strings",
			src: schemas(composes("M", "D0", "D1", "D2"),
				"D0: {type: object, properties: {p: {type: array, description: d, items: {type: string}}}}",
				"D1: {type: object, properties: {p: {type: array, items: {type: string, maxLength: 3}}}}",
				"D2: {type: object, properties: {p: {type: integer}}}"),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/D2/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// The deepest chain the permutation admits: its canonical form
			// nests within two source bounds whichever link is met first.
			name: "a chain of 255 schemas, each a property under an allOf",
			src:  propertyChain(255),
			want: passed(OracleSequenceOrder),
		},
		{
			// Both declare the same member, so which one the model takes is no
			// difference a consumer can see.
			name: "two declarations saying the same",
			src: schemas("A: {type: object, properties: {p: {type: string}}}",
				"B: {type: object, properties: {p: {type: string}}}",
				"AB: {allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}]}"),
			want: passed(OracleSequenceOrder),
		},
		{
			// The two entries name each other through their $id, which no
			// pointer spells, so the allOf is reversed. Which of the two the
			// comparison meets first decides their digests, and reversing the
			// entries changes which, so each order found the other different.
			name: "two nodes beneath an allOf naming each other",
			src: schemas("X: {allOf: [{$id: 'https://example.test/a', type: object, properties: " +
				"{toB: {$ref: 'https://example.test/b'}, x: {type: string}}}, " +
				"{$id: 'https://example.test/b', type: object, properties: {toA: {$ref: 'https://example.test/a'}, y: {type: integer}}}]}"),
			kept: []string{`"target":"type/anonymous/components/schemas/X/allOf/1"`,
				`"target":"type/anonymous/components/schemas/X/allOf/0"`},
			want: declined(OracleSequenceOrder, DeclineReferenceCycle),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, diags, err := compile(context.Background(), "spec.yaml", []byte(tc.src), passes())
			require.NoError(t, err)
			require.NotNil(t, doc, "the fixture must compile to a document")
			encoded, err := json.Marshal(doc)
			require.NoError(t, err)
			for _, k := range tc.kept {
				assert.Contains(t, string(encoded), k, "the fixture must keep what the row is about")
			}
			for code, pointer := range tc.codes {
				assert.Contains(t, diagnosticsAt(diags, code), pointer, "the compile must report %s there", code)
			}

			got := sequenceVerdict(t, Check(context.Background(), "spec.yaml", []byte(tc.src)))

			assert.Equal(t, tc.want, got)
		})
	}
}

// listsOfModels writes the components I1 and I2, declaring p an integer, and
// LX and LY, declaring it a non-empty list of X and of Y, whose q is xq and yq:
// the two lists meet in a list of a model composing X and Y, which ties with
// the two integers.
func listsOfModels(xq, yq string) string {
	return strings.Join([]string{
		"X: {type: object, properties: {q: " + xq + "}}",
		"Y: {type: object, properties: {q: " + yq + "}}",
		"I1: {type: object, properties: {p: {type: integer}}}",
		"I2: {type: object, properties: {p: {type: integer}}}",
		"LX: {type: object, properties: {p: {type: array, minItems: 1, items: {$ref: '#/components/schemas/X'}}}}",
		"LY: {type: object, properties: {p: {type: array, minItems: 1, items: {$ref: '#/components/schemas/Y'}}}}",
	}, "\n    ")
}

// TestCheck_SequenceOrderOracleDeclinesASpecOfTwoFiles pins that a source
// whose compile reads another file is declined: that file names the first
// entry of N's allOf by its index, which the walk does not read, so reversing
// the entries would change what the reference names, not only their order.
func TestCheck_SequenceOrderOracleDeclinesASpecOfTwoFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := schemas("N: {allOf: [{type: object, properties: {p: {type: number}}}, {type: object, properties: {p: {type: integer}}}]}",
		"User: {$ref: 'other.yaml#/Thing'}")
	spec := filepath.Join(dir, "main.yaml")
	require.NoError(t, os.WriteFile(spec, []byte(src), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"),
		[]byte("Thing: {type: object, properties: {branch: {$ref: 'main.yaml#/components/schemas/N/allOf/0'}}}\n"), 0o600))

	got := sequenceVerdict(t, Check(t.Context(), spec, []byte(src)))

	assert.Equal(t, declined(OracleSequenceOrder, DeclineReadsAnotherFile), got)
}

// propertyChain is a document whose one schema nests n levels, each an allOf
// of an object with a property p holding the next and a sibling with q.
func propertyChain(n int) string {
	schema := `{"type": "string"}`
	for range n {
		schema = `{"allOf": [{"type": "object", "properties": {"p": ` + schema +
			`}}, {"type": "object", "properties": {"q": {"type": "string"}}}]}`
	}
	return `{"openapi": "3.1.0", "info": {"title": "t", "version": "1"}, "paths": {}, ` +
		`"components": {"schemas": {"X": ` + schema + `}}}`
}

// TestSequenceOrderInvariant_GivesOneVerdictOnEveryRun pins that the depth
// the canonical form reaches is a function of the document. The chain's links
// sit side by side, so the permutation's bound does not cap their number, and
// each link's digest nests through the next: a rewrite meeting them in a map's
// order passed the spec on some runs and declined it as too deep on others.
func TestSequenceOrderInvariant_GivesOneVerdictOnEveryRun(t *testing.T) {
	t.Parallel()
	src := []byte(anchorChain(500))

	first := sequenceOrderInvariant(context.Background(), "spec.json", src, true)
	for range 19 {
		assert.Equal(t, first, sequenceOrderInvariant(context.Background(), "spec.json", src, true))
	}
}

// anchorChain is a spec whose allOf's first entry names the first of n+1
// schemas it defines, each but the last an object whose property names the
// next by its $anchor.
func anchorChain(n int) string {
	defs := make([]string, 0, n+1)
	for i := range n {
		defs = append(defs, fmt.Sprintf(`"d%d": {"$anchor": "a%d", "type": "object", "properties": {"n": {"$ref": "#a%d"}}}`, i, i, i+1))
	}
	defs = append(defs, fmt.Sprintf(`"d%d": {"$anchor": "a%d", "type": "string"}`, n, n))
	return `{"openapi": "3.1.0", "info": {"title": "t", "version": "1"}, "paths": {}, "components": {"schemas": {"X": ` +
		`{"allOf": [{"type": "object", "properties": {"start": {"$ref": "#a0"}}, "$defs": {` + strings.Join(defs, ", ") + `}}, ` +
		`{"type": "object", "properties": {"q": {"type": "string"}}}]}}}}`
}

// diagnosticsAt is the pointers of the diagnostics of one code.
func diagnosticsAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, diagnostic := range diags {
		if diagnostic.Code == code {
			out = append(out, string(diagnostic.Provenance.Pointer))
		}
	}
	return out
}

// TestCheck_SequenceOrderOracleAsksOfAStreamWithADroppedDocument pins that a
// stream whose second document the compiler drops is asked of, rather than
// declined whole: only the first document is compiled, but a sequence of it is
// still the oracle's to swap.
func TestCheck_SequenceOrderOracleAsksOfAStreamWithADroppedDocument(t *testing.T) {
	src := schemas("N: {allOf: [{type: object, properties: {p: {type: array}}}, "+
		"{type: object, properties: {p: {type: array, items: {type: string}}}}]}") +
		"---\nother: {allOf: [{a: 1}, {b: 2}]}\n"

	got := sequenceVerdict(t, Check(context.Background(), "spec.yaml", []byte(src)))

	assert.True(t, got.Asked(), "declined: %s", got.Declined)
}

// planted is a change the compile seam makes to the document a compile
// returns, standing for a defect the real compiler does not have on a fixture
// this package can pin.
type planted func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error)

// plant makes every compile return what the real compile returns, changed by
// edit. A source is the reversed one when it names beta before alpha.
func plant(t *testing.T, edit planted) {
	t.Helper()
	var orig compileFunc
	orig = swapCompile(t, func(ctx context.Context, spec string, data []byte, ps []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		doc, diags, err := orig(ctx, spec, data, ps)
		if err != nil {
			return doc, diags, err
		}
		reversed := bytes.Index(data, []byte("beta")) < bytes.Index(data, []byte("alpha"))
		return edit(t, reversed, doc, diags)
	})
}

// Fixtures of the detection rows, each passing the oracle as the real
// compiler lowers it.
var (
	// fixtureX: two entries declaring different members.
	fixtureX = schemas("AB: {allOf: [{type: object, properties: {alpha: {type: string}}}, " +
		"{type: object, properties: {beta: {type: integer}}}]}")
	// fixtureY: both entries declare p, the same way.
	fixtureY = schemas("AB: {allOf: [{type: object, properties: {p: {type: string}, alpha: {type: string}}}, " +
		"{type: object, properties: {p: {type: string}, beta: {type: integer}}}]}")
	// fixtureMixins: two named conjuncts, which the model composes as mixins.
	fixtureMixins = schemas("AB: {allOf: [{$ref: '#/components/schemas/alpha'}, {$ref: '#/components/schemas/beta'}]}",
		"alpha: {type: object, properties: {x: {type: string}}}", "beta: {type: object, properties: {y: {type: string}}}")
	// fixtureKept: an allOf the model keeps verbatim, under not.
	fixtureKept = schemas("AB: {type: object, not: {allOf: [{required: [alpha]}, {required: [beta]}]}}")
	// fixtureZ: both entries declare p, as an object of their own.
	fixtureZ = schemas("AB: {allOf: [{type: object, properties: {p: {type: object, properties: {x: {type: string}}}, alpha: {type: string}}}, " +
		"{type: object, properties: {p: {type: object, properties: {y: {type: string}}}, beta: {type: string}}}]}")
	// fixtureBeneath: AB's p ties alpha's string and an object AB writes, so
	// the object survives only in the order where AB's declaration wins.
	fixtureBeneath = schemas("AB: {allOf: [{$ref: '#/components/schemas/alpha'}, "+
		"{type: object, properties: {p: {type: object, properties: {beta: {type: string}}}}}]}",
		"alpha: {type: object, properties: {p: {type: string}}}")
	// fixtureBeneathKept: AB's p composes alpha's object and an inline object
	// AB writes while their descriptions tie, so AB's inline object stays
	// referenced in either order and what it says is compared.
	fixtureBeneathKept = schemas("AB: {allOf: [{$ref: '#/components/schemas/alpha'}, "+
		"{type: object, properties: {p: {type: object, description: b, properties: {beta: {type: string}}}}}]}",
		"alpha: {type: object, properties: {p: {type: object, description: a, properties: {gamma: {type: string}}}}}")
	// fixtureSwap: two models composing alpha and beta, whose p disagree, so
	// the one report of the tie moves from one declaration to the other.
	// Their q agree.
	fixtureSwap = schemas("AB: {allOf: [{$ref: '#/components/schemas/alpha'}, {$ref: '#/components/schemas/beta'}], "+
		"properties: {r: {type: string}}}",
		"AB2: {allOf: [{$ref: '#/components/schemas/alpha'}, {$ref: '#/components/schemas/beta'}]}",
		"alpha: {type: object, properties: {p: {type: string}, q: {type: string}}}",
		"beta: {type: object, properties: {p: {type: integer}, q: {type: string}}}")
	// fixtureBeside: fixtureSwap's tie over alpha and beta, and a model
	// composing beta and gamma, whose p agree.
	fixtureBeside = schemas("AB: {allOf: [{$ref: '#/components/schemas/alpha'}, {$ref: '#/components/schemas/beta'}]}",
		"AB2: {allOf: [{$ref: '#/components/schemas/alpha'}, {$ref: '#/components/schemas/beta'}]}",
		"BG: {allOf: [{$ref: '#/components/schemas/beta'}, {$ref: '#/components/schemas/gamma'}]}",
		"alpha: {type: object, properties: {p: {type: string}}}", "beta: {type: object, properties: {p: {type: integer}}}",
		"gamma: {type: object, properties: {p: {type: integer}}}")
	// fixtureThree: two models composing A, B and C, where A and B disagree
	// and C gives p a description, so p takes what neither A nor B says
	// alone.
	fixtureThree = schemas(composesAll("M1", "A", "B", "C"), composesAll("M2", "A", "B", "C"),
		"A: {type: object, properties: {p: {type: string}}}", "B: {type: object, properties: {p: {type: integer}}}",
		"C: {type: object, properties: {p: {description: from C}}}")
	// fixtureDescTrade: two families composing A and B in either order,
	// trading their ties, where A also gives p a description both keep and
	// M2 requires p.
	fixtureDescTrade = schemas(composesAll("M1", "A", "B"),
		"M2: {allOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}], required: [p]}",
		composesAll("N1", "B", "A"), composesAll("N2", "B", "A"),
		"A: {type: object, properties: {p: {type: string, description: da}}}", "B: {type: object, properties: {p: {type: integer}}}")
	// fixtureFar: a model composing beta and gamma, where gamma narrows beta,
	// beside ties over alpha and beta and over gamma and delta, which report
	// at gamma as written and at beta reversed.
	fixtureFar = schemas(composesAll("AB1", "alpha", "beta"), composesAll("AB2", "alpha", "beta"),
		composesAll("GD1", "gamma", "delta"), composesAll("GD2", "gamma", "delta"), composesAll("BG", "beta", "gamma"),
		"alpha: {type: object, properties: {p: {type: string}}}", "beta: {type: object, properties: {p: {type: integer}}}",
		"gamma: {type: object, properties: {p: {type: integer, minimum: 1}}}",
		"delta: {type: object, properties: {p: {type: boolean}}}")
	// fixtureTrade: two families composing alpha and beta in either order,
	// whose ties trade declarations, each reporting at both in either order.
	fixtureTrade = schemas(composesAll("AB1", "alpha", "beta"), composesAll("AB2", "alpha", "beta"),
		composesAll("BA1", "beta", "alpha"), composesAll("BA2", "beta", "alpha"),
		"alpha: {type: object, properties: {p: {type: string}}}", "beta: {type: object, properties: {p: {type: integer}}}")
	// fixtureAgreeing: W composing alpha, beta and gamma, where gamma agrees
	// with alpha, so beta's p loses in either order and no other model reports.
	fixtureAgreeing = schemas(composesAll("W", "alpha", "beta", "gamma"),
		"alpha: {type: object, properties: {p: {type: string}}}", "beta: {type: object, properties: {p: {type: integer}}}",
		"gamma: {type: object, properties: {p: {type: string}}}")
	// fixtureMeeting: W composing alpha, beta and gamma, where alpha's and
	// gamma's lists meet in one the fold mints, so beta's p loses in either
	// order, and its report keeps whichever list is written later.
	fixtureMeeting = schemas(composesAll("W", "alpha", "beta", "gamma"),
		"alpha: {type: object, properties: {p: {type: array, minItems: 1, items: {type: string}}}}",
		"beta: {type: object, properties: {p: {type: integer}}}",
		"gamma: {type: object, properties: {p: {type: array, maxItems: 5, items: {type: string}}}}")
	// fixtureOutvoted: fixtureTrade's two families, and W composing beta,
	// alpha and gamma, where gamma agrees with alpha, so beta's p loses in
	// either order.
	fixtureOutvoted = schemas(composesAll("AB1", "alpha", "beta"), composesAll("AB2", "alpha", "beta"),
		composesAll("BA1", "beta", "alpha"), composesAll("BA2", "beta", "alpha"), composesAll("W", "beta", "alpha", "gamma"),
		"alpha: {type: object, properties: {p: {type: string}}}", "beta: {type: object, properties: {p: {type: integer}}}",
		"gamma: {type: object, properties: {p: {type: string}}}")
	// fixtureGroup: a tie one model decides between alpha and beta, beside a
	// model composing them and gamma, which agrees with alpha, so beta's p
	// loses there in either order: reversed, one report speaks for both.
	fixtureGroup = schemas(composesAll("AB", "alpha", "beta"), composesAll("ABG", "alpha", "beta", "gamma"),
		"alpha: {type: object, properties: {p: {type: string}}}", "beta: {type: object, properties: {p: {type: integer}}}",
		"gamma: {type: object, properties: {p: {type: string}}}")
	// fixtureBrought: M composes X, which declares p inline three ways, two
	// of them alike, and beta: X brings p named by its first declaration,
	// which the reversal moves to another entry, and M's tie is between that
	// member and beta's p.
	fixtureBrought = schemas("X: {allOf: [{type: object, properties: {p: {type: string}}}, "+
		"{type: object, properties: {p: {type: boolean}}}, {$ref: '#/components/schemas/alpha'}]}",
		composesAll("M", "X", "beta"),
		"alpha: {type: object, properties: {p: {type: string}}}", "beta: {type: object, properties: {p: {type: integer}}}")
	// fixtureLoser: fixtureSwap's tie over alpha and beta, a tie over beta and
	// delta, so beta's p loses as written and reversed, a model composing beta
	// alone, which holds that declaration as beta does, and one composing beta
	// and gamma, whose p agree.
	fixtureLoser = schemas("AB: {allOf: [{$ref: '#/components/schemas/alpha'}, {$ref: '#/components/schemas/beta'}]}",
		"AB2: {allOf: [{$ref: '#/components/schemas/alpha'}, {$ref: '#/components/schemas/beta'}]}",
		"BD: {allOf: [{$ref: '#/components/schemas/beta'}, {$ref: '#/components/schemas/delta'}]}",
		"BD2: {allOf: [{$ref: '#/components/schemas/beta'}, {$ref: '#/components/schemas/delta'}]}",
		"B: {allOf: [{$ref: '#/components/schemas/beta'}]}",
		"BG: {allOf: [{$ref: '#/components/schemas/beta'}, {$ref: '#/components/schemas/gamma'}]}",
		"alpha: {type: object, properties: {p: {type: string}}}", "beta: {type: object, properties: {p: {type: integer}}}",
		"delta: {type: object, properties: {p: {type: boolean}}}", "gamma: {type: object, properties: {p: {type: integer}}}")
)

// composesAll is a schema named name whose allOf references each of
// entries.
func composesAll(name string, entries ...string) string {
	refs := make([]string, len(entries))
	for i, e := range entries {
		refs[i] = "{$ref: '#/components/schemas/" + e + "'}"
	}
	return name + ": {allOf: [" + strings.Join(refs, ", ") + "]}"
}

// abID is the model every detection fixture declares.
const abID ir.TypeID = "type/openapi/components/schemas/AB"

// property is the member of AB with the wire name.
func property(t *testing.T, doc *ir.Document, wire string) *ir.Property {
	t.Helper()
	model, ok := doc.Types[abID].(*ir.Model)
	require.True(t, ok, "the fixture must lower AB to a model")
	for i := range model.Properties {
		if model.Properties[i].WireName == wire {
			return &model.Properties[i]
		}
	}
	require.FailNow(t, "AB declares no member", wire)
	return nil
}

// diagnostic is a diagnostic of the planted kind.
func diagnostic(sev ir.Severity, code, pointer, node string) ir.Diagnostic {
	return ir.Diagnostic{Severity: sev, Code: code, Message: "planted",
		Provenance: ir.Provenance{Pointer: jsontext.Pointer(pointer), Node: node}}
}

// TestSequenceOrderInvariant_FindsWhatDiffers plants, through the compile seam,
// each way a lowering can depend on the order of a sequence, and holds the
// oracle to finding it, and to forgiving a report that moves with the order.
func TestSequenceOrderInvariant_FindsWhatDiffers(t *testing.T) {
	const tie = "/components/schemas/AB/allOf/1/properties/p"
	integer := func(wire string) planted {
		return func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
			if reversed {
				property(t, doc, wire).Type.Target = "type/primitive/integer"
			}
			return doc, diags, nil
		}
	}
	tied := func(inner planted) planted {
		return func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
			diags = append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/tie", tie, ""))
			return inner(t, reversed, doc, diags)
		}
	}
	tests := []struct {
		name     string
		src      string
		edit     planted
		found    bool
		contains []string
		not      []string
		// unread says the source as given was not compiled, so a refusal as
		// written is compared rather than declined.
		unread bool
	}{
		{
			name:  "a member whose type follows the order",
			src:   fixtureX,
			edit:  integer("alpha"),
			found: true,
			contains: []string{
				"document depends on sequence order",
				"flattened shape depends on sequence order",
			},
		},
		{
			name: "a report only the reversed compile makes",
			src:  fixtureX,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					diags = append(slices.Clip(diags),
						diagnostic(ir.SeverityInfo, "x/planted", "/components/schemas/AB", ""))
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/planted"},
		},
		{
			// The report moves with the order, so p is a tie site and what the
			// document holds beneath it is not compared.
			name: "a reported tie is forgiven",
			src:  fixtureY,
			edit: tied(func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					property(t, doc, "p").Docs.Description = "reversed"
				}
				return doc, diags, nil
			}),
		},
		{
			// The tie covers p alone: a difference elsewhere is still one.
			name:     "a reported tie forgives nothing outside its site",
			src:      fixtureY,
			edit:     tied(integer("alpha")),
			found:    true,
			contains: []string{"alpha"},
			not:      []string{`"wireName": "p"`},
		},
		{
			// Both orders name allOf/0's p, which is a different node in each,
			// so the two IDs are one string and two things.
			name: "a type naming an inline node by its position",
			src:  fixtureZ,
			edit: func(t *testing.T, _ bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				property(t, doc, "p").Type.Target = "type/anonymous/components/schemas/AB/allOf/0/properties/p"
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order"},
		},
		{
			// Two reports differing in the node they name alone move no
			// pointer, so they mark no tie site.
			name: "two reports differing only in their node",
			src:  fixtureX,
			edit: func(_ *testing.T, _ bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				diags = append(slices.Clip(diags),
					diagnostic(ir.SeverityInfo, "x/node", "", "type/anonymous/components/schemas/AB/allOf/0"))
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/node"},
		},
		{
			// The mixins compare as a multiset, not as a set.
			name: "a mixin listed twice in one order",
			src:  fixtureMixins,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				model, ok := doc.Types[abID].(*ir.Model)
				require.True(t, ok, "the fixture must lower AB to a model")
				require.Len(t, model.Mixins, 2, "the fixture must compose two mixins")
				if reversed {
					model.Mixins = append(model.Mixins, model.Mixins[0])
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order"},
		},
		{
			// A kept allOf compares as a multiset of its entries.
			name: "an entry of a kept allOf listed twice in one order",
			src:  fixtureKept,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				model, ok := doc.Types[abID].(*ir.Model)
				require.True(t, ok, "the fixture must lower AB to a model")
				entry, kept := model.Unmodeled["openapi:not"]
				require.True(t, kept, "the fixture must keep not verbatim")
				if reversed {
					entry.Value = ir.RawValue(`{"allOf":[{"required":["beta"]},{"required":["alpha"]},{"required":["alpha"]}]}`)
					model.Unmodeled = ir.Unmodeled{"openapi:not": entry}
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order"},
		},
		{
			// The two reports differ in which member they name, not only in
			// which entry, so they are no tie.
			name: "a report moving to another member",
			src:  fixtureY,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				at := tie
				if reversed {
					at = "/components/schemas/AB/allOf/1/properties/beta"
				}
				return doc, append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/tie", at, "")), nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/tie"},
		},
		{
			// The second document reverses X's allOf, which the first, the one
			// compiled, writes with one entry. A report there in both orders
			// moves nothing, so it marks no tie over what differs beneath it.
			name: "a difference beneath a path another document of the stream writes",
			src: fixtureX + "    X: {allOf: [{type: object, properties: {p: {type: string}}}]}\n---\n" +
				schemas("X: {allOf: [{title: a}, {title: b}]}"),
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				model, ok := doc.Types["type/openapi/components/schemas/X"].(*ir.Model)
				require.True(t, ok, "the fixture must lower X to a model")
				require.Len(t, model.Properties, 1, "the fixture must fold X's one entry into it")
				if reversed {
					model.Properties[0].Type.Target = "type/primitive/integer"
				}
				return doc, append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/at", "/components/schemas/X/allOf/0", "")), nil
			},
			found:    true,
			contains: []string{"document depends on sequence order"},
		},
		{
			// What a member takes of two declarations that disagree follows
			// the tie its reports state: its type, and the bounds and values
			// beside it.
			name: "a tie two reports state forgives what its member takes of the declarations",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					property(t, doc, "p").Type.Target = "type/primitive/bool"
					property(t, doc, "p").Constraints = &ir.Constraints{MinLength: new(int64(1))}
				}
				return doc, diags, nil
			},
		},
		{
			// A member's docs fold over every declaration, and two giving one
			// differently draw a report of their own: the tie between
			// alpha's and beta's types decides none of it.
			name: "a tied member's description in one order alone",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					property(t, doc, "p").Docs.Description = "reversed"
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `"description": "reversed"`, `member "p" reported tie at`},
		},
		{
			name:     "a report a tie moves between declarations forgives no other member",
			src:      fixtureSwap,
			edit:     integer("r"),
			found:    true,
			contains: []string{"document depends on sequence order", `"wireName":"r"`},
		},
		{
			// A declaration stands on its own model, which no tie decides.
			name: "a report a tie moves between declarations forgives neither declaration",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				alpha, ok := doc.Types["type/openapi/components/schemas/alpha"].(*ir.Model)
				require.True(t, ok, "the fixture must lower alpha to a model")
				if reversed {
					alpha.Properties[0].Docs.Description = "reversed"
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `alpha member "p"`},
		},
		{
			// No member folds alpha's q with beta's p.
			name: "a report moving between declarations no member folds together",
			src:  fixtureSwap,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				at := "/components/schemas/alpha/properties/q"
				if reversed {
					at = "/components/schemas/beta/properties/p"
				}
				return doc, append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/swap", at, "")), nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/swap"},
		},
		{
			// A report carrying no pair states no tie, though it moves
			// between the declarations of one.
			name: "reports naming no pair moving between a tie's declarations",
			src:  fixtureSwap,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				irDiagnostic := diagnostic(ir.SeverityWarning, "x/swap", "/components/schemas/alpha/properties/p", "")
				planted := []ir.Diagnostic{irDiagnostic, irDiagnostic}
				if reversed {
					planted = []ir.Diagnostic{diagnostic(ir.SeverityWarning, "x/swap", "/components/schemas/beta/properties/p", "")}
				}
				return doc, append(slices.Clip(diags), planted...), nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/swap"},
		},
		{
			name: "a report of another code moving between a tie's declarations",
			src:  fixtureSwap,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				at := "/components/schemas/alpha/properties/p"
				if reversed {
					at = "/components/schemas/beta/properties/p"
				}
				return doc, append(slices.Clip(diags), diagnostic(ir.SeverityInfo, "x/other", at, "")), nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/other"},
		},
		{
			// W composes beta, alpha and gamma, where alpha and gamma agree,
			// so no tie decides its p, which the report says lost beta in
			// either order. The ties the other models trade forgive none of it.
			name: "a member no tie decides taking another type beside ties two models trade",
			src:  fixtureOutvoted,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				model, ok := doc.Types["type/openapi/components/schemas/W"].(*ir.Model)
				require.True(t, ok, "the fixture must lower W to a model")
				require.Len(t, model.Properties, 1, "the fixture must fold W's p into W")
				if !reversed {
					model.Properties[0].Type.Target = "type/primitive/integer"
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `W member "p"`},
		},
		{
			// AB's p takes alpha's type where its report says alpha lost, and
			// beta's where it says beta did.
			name: "a tied member taking in each order the type its report says lost",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				property(t, doc, "p").Type.Target = "type/primitive/string"
				if reversed {
					property(t, doc, "p").Type.Target = "type/primitive/integer"
				}
				return doc, diags, nil
			},
			found: true,
			contains: []string{
				"a tie's reports name the declaration the member takes as the one that lost",
				"property/composed/components/schemas/AB/allOf/properties/p takes in each order",
			},
			not: []string{"AB2"},
		},
		{
			// A member is required where any declaration says so, which no
			// tie between two of them decides.
			name: "a tied member required in one order alone",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				property(t, doc, "p").Required = reversed
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `"required":`, "flattened shape depends on sequence order"},
		},
		{
			// The one report said for both models lists AB2's p in one order
			// alone.
			name: "a conflict listing a member it decided in one order alone",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					return doc, diags, nil
				}
				out := slices.Clone(diags)
				dropped := 0
				for i, diagnostic := range out {
					out[i].Related = slices.DeleteFunc(slices.Clone(diagnostic.Related), func(related ir.Related) bool {
						return strings.Contains(related.Provenance.Node, "/AB2/")
					})
					dropped += len(diagnostic.Related) - len(out[i].Related)
				}
				require.Equal(t, 1, dropped, "the fixture must list AB2's p on its one conflict")
				return doc, out, nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "openapi/conflicting-redeclaration", "flattened shape depends on sequence order"},
		},
		{
			// A member is secret where any declaration says so, which no tie
			// between two of them decides.
			name: "a tied member secret in one order alone",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				property(t, doc, "p").Secret = reversed
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `"secret": true`, "flattened shape depends on sequence order"},
		},
		{
			// A member's deprecation folds over every declaration, as its docs
			// do, and the tie between alpha's and beta's types decides none of it.
			name: "a tied member's deprecation in one order alone",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					property(t, doc, "p").Deprecation = &ir.Deprecation{Message: "reversed"}
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `"message": "reversed"`, "flattened shape depends on sequence order"},
		},
		{
			// W's p loses beta's declaration in either order, and its report
			// keeps gamma's as written and alpha's reversed, which name one
			// type. Naming beta's, of another type, in one order is found.
			name: "a report naming as kept, in one order, a declaration of another type",
			src:  fixtureAgreeing,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if !reversed {
					return doc, diags, nil
				}
				out := slices.Clone(diags)
				renamed := 0
				for i, diagnostic := range out {
					if len(diagnostic.Related) == 2 && strings.Contains(diagnostic.Related[1].Provenance.Node, "/W/") {
						out[i].Related = slices.Clone(diagnostic.Related)
						out[i].Related[0].Provenance.Pointer = "/components/schemas/beta/properties/p"
						renamed++
					}
				}
				require.Equal(t, 1, renamed, "the fixture must report W's p alone")
				return doc, out, nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", `kept\x00/components/schemas/beta/properties/p`},
			not:      []string{"document depends on sequence order"},
		},
		{
			// The stated limit: where W's p takes the list the fold mints, its
			// report's kept place is read as that list, so naming beta's
			// declaration, which the fold dropped, in one order passes.
			name: "a report naming as kept, in one order, a dropped declaration where the fold mints",
			src:  fixtureMeeting,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if !reversed {
					return doc, diags, nil
				}
				out := slices.Clone(diags)
				renamed := 0
				for i, diagnostic := range out {
					if len(diagnostic.Related) == 2 && strings.Contains(diagnostic.Related[1].Provenance.Node, "/W/") {
						out[i].Related = slices.Clone(diagnostic.Related)
						out[i].Related[0].Provenance.Pointer = "/components/schemas/beta/properties/p"
						renamed++
					}
				}
				require.Equal(t, 1, renamed, "the fixture must report W's p alone")
				return doc, out, nil
			},
			found: false,
		},
		{
			// The object AB writes for p is referenced in either order (by the
			// composed p model), while p's description ties, so what it says
			// is compared.
			name: "a node written where a tied member stands, differing",
			src:  fixtureBeneathKept,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				const written = "type/anonymous/components/schemas/AB/allOf/0/properties/p"
				if !reversed {
					return doc, diags, nil
				}
				model, ok := doc.Types[written].(*ir.Model)
				require.True(t, ok, "the fixture must write p's object at AB's first entry, reversed")
				model.Docs.Description = "reversed"
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `"description": "reversed"`},
		},
		{
			// The two reports are mirrored but say the conflict at another
			// severity, so they state no tie, and what the members take of
			// the two declarations is compared.
			name: "a tie's report at another severity in one order",
			src:  fixtureSwap,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if !reversed {
					return doc, diags, nil
				}
				out := slices.Clone(diags)
				for i := range out {
					if out[i].Code == "openapi/conflicting-redeclaration" {
						out[i].Severity = ir.SeverityInfo
					}
				}
				return doc, out, nil
			},
			found: true,
			contains: []string{
				"diagnostics depend on sequence order", `+"info\x00openapi/conflicting-redeclaration`,
				"document depends on sequence order", `AB member "p"`,
			},
		},
		{
			// q folds alpha's and beta's declarations, which say the same, so
			// no tie moves a report between them.
			name: "a report moving between the declarations of a member declared alike",
			src:  fixtureSwap,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				at := "/components/schemas/alpha/properties/q"
				if reversed {
					at = "/components/schemas/beta/properties/q"
				}
				return doc, append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/swap", at, "")), nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/swap"},
		},
		{
			// q folds two declarations and differs, but no report says a tie
			// decided it.
			name: "a member folding two declarations differing where no report says so",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					property(t, doc, "q").Docs.Description = "reversed"
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `member "q"`},
		},
		{
			// A tie moves the report at the declaration that lost: AB.q differs
			// while the report beside it sits at alpha's q in either order.
			name: "a member differing where the report at its declarations stays put",
			src:  fixtureSwap,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					property(t, doc, "q").Docs.Description = "reversed"
				}
				return doc, append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/stay", "/components/schemas/alpha/properties/q", "")), nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `member "q"`},
		},
		{
			// BG's p folds beta's declaration, which the tie of AB and AB2
			// reports at reversed, and gamma's, which says the same: no order
			// reports at BG.p's declarations as written, so no tie is shown to
			// decide it.
			name: "a member sharing a declaration with a tie differing where its declarations hold no report",
			src:  fixtureBeside,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				bg, ok := doc.Types["type/openapi/components/schemas/BG"].(*ir.Model)
				require.True(t, ok, "the fixture must lower BG to a model")
				require.Len(t, bg.Properties, 1, "the fixture must fold BG's p into BG")
				if reversed {
					bg.Properties[0].Docs.Description = "reversed"
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `BG member "p"`},
		},
		{
			// beta's own p is one declaration, which two models' flattenings
			// hold and the ties report at in either order: no tie decides it.
			name: "a member of one declaration differing where ties report at it in either order",
			src:  fixtureLoser,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				beta, ok := doc.Types["type/openapi/components/schemas/beta"].(*ir.Model)
				require.True(t, ok, "the fixture must lower beta to a model")
				require.Len(t, beta.Properties, 1, "the fixture must declare beta's p")
				if reversed {
					beta.Properties[0].Docs.Description = "reversed"
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `beta member "p"`},
		},
		{
			// BG's p folds beta's declaration, at which the two ties report in
			// either order, and gamma's, which says the same and at which no
			// order reports: no tie moves a report between BG.p's declarations.
			name: "a member folding a declaration ties report at in either order differing",
			src:  fixtureLoser,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				bg, ok := doc.Types["type/openapi/components/schemas/BG"].(*ir.Model)
				require.True(t, ok, "the fixture must lower BG to a model")
				require.Len(t, bg.Properties, 1, "the fixture must fold BG's p into BG")
				if reversed {
					bg.Properties[0].Docs.Description = "reversed"
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `BG member "p"`},
		},
		{
			// BG's p takes gamma's narrower type in either order, while the
			// ties beside it report at gamma as written and at beta reversed:
			// the reports change at its declarations, never where it does not
			// take the type.
			name: "a member taking one type in either order beside other ties' moving reports",
			src:  fixtureFar,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				bg, ok := doc.Types["type/openapi/components/schemas/BG"].(*ir.Model)
				require.True(t, ok, "the fixture must lower BG to a model")
				require.Len(t, bg.Properties, 1, "the fixture must fold BG's p into BG")
				if reversed {
					bg.Properties[0].Docs.Description = "reversed"
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `BG member "p"`},
		},
		{
			// Each order reports the tie at the declaration p takes, not at
			// the one it lost.
			name: "a conflict reported at the declaration that won",
			src:  fixtureSwap,
			edit: func(t *testing.T, _ bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				const alpha, beta = "/components/schemas/alpha/properties/p", "/components/schemas/beta/properties/p"
				out := slices.Clone(diags)
				for i, diagnostic := range out {
					switch {
					case diagnostic.Code != "openapi/conflicting-redeclaration":
					case diagnostic.Provenance.Pointer == alpha:
						out[i].Provenance.Pointer = beta
					case diagnostic.Provenance.Pointer == beta:
						out[i].Provenance.Pointer = alpha
					}
				}
				require.NotEqual(t, diags, out, "the fixture must report the tie")
				return doc, out, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", `member "p"`},
		},
		{
			// The AB models' and BA models' ties trade declarations; as
			// written the BA models' report at beta is missing, though AB's
			// at alpha stands for it by its key.
			name: "a conflict two ties trading declarations miss in one order",
			src:  fixtureTrade,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					return doc, diags, nil
				}
				out := slices.DeleteFunc(slices.Clone(diags), func(diagnostic ir.Diagnostic) bool {
					return diagnostic.Provenance.Pointer == "/components/schemas/beta/properties/p"
				})
				require.Len(t, out, len(diags)-1, "the fixture must report the BA models' tie at beta")
				return doc, out, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order", "flattened shape depends on sequence order"},
		},
		{
			// q's declarations differ in whether their models require it
			// alone, which no tie decides, so a report moving between them
			// ties nothing.
			name: "a report moving between declarations differing only in what their models require",
			src: schemas(composesAll("AB", "alpha", "beta"), composesAll("AB2", "alpha", "beta"),
				"alpha: {type: object, required: [q], properties: {p: {type: string}, q: {type: string}}}",
				"beta: {type: object, properties: {p: {type: integer}, q: {type: string}}}"),
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				at := "/components/schemas/alpha/properties/q"
				if reversed {
					at = "/components/schemas/beta/properties/q"
					property(t, doc, "q").Docs.Description = "reversed"
				}
				return doc, append(slices.Clip(diags), diagnostic(ir.SeverityWarning, "x/swap", at, "")), nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/swap", `member "q"`},
		},
		{
			name: "a report moving between declarations that changes more than its pointer",
			src:  fixtureSwap,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				irDiagnostic := diagnostic(ir.SeverityWarning, "x/swap", "/components/schemas/alpha/properties/p", "")
				if reversed {
					irDiagnostic = diagnostic(ir.SeverityInfo, "x/swap", "/components/schemas/beta/properties/p", "")
				}
				return doc, append(slices.Clip(diags), irDiagnostic), nil
			},
			found:    true,
			contains: []string{"diagnostics depend on sequence order", "x/swap"},
		},
		{
			// Only the reversed compile holds models, and a report the written
			// one lacks: there is no document as written to pair it through.
			name: "a source compiling as written to no document",
			src:  fixtureSwap,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if !reversed {
					return nil, []ir.Diagnostic{diagnostic(ir.SeverityError, "x/refused", "", "")}, nil
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"the permuted source compiles to a document, and as written to none"},
			unread:   true,
		},
		{
			name: "a permuted source compiling to no document",
			src:  fixtureX,
			edit: func(_ *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				if reversed {
					return nil, []ir.Diagnostic{diagnostic(ir.SeverityError, "x/refused", "", "")}, nil
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"the permuted source compiles to no document, and as written to one"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plant(t, tc.edit)

			got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(tc.src), !tc.unread)

			if !tc.found {
				assert.Equal(t, passed(OracleSequenceOrder), got)
				return
			}
			require.True(t, got.Found(), "the oracle must find it: %+v", got)
			assert.Equal(t, OracleSequenceOrder, got.Oracle)
			assert.Equal(t, OutcomeSequenceOrderDependent, got.Outcome)
			for _, c := range tc.contains {
				assert.Contains(t, got.Detail, c)
			}
			for _, n := range tc.not {
				assert.NotContains(t, got.Detail, n)
			}
		})
	}
}

// TestSequenceOrderInvariant_UnplantedFixturesPass is the control of the
// detection rows: each fixture, compiled by the real compiler in both orders,
// passes, so a finding above is the plant's.
func TestSequenceOrderInvariant_UnplantedFixturesPass(t *testing.T) {
	for name, src := range map[string]string{"X": fixtureX, "Y": fixtureY, "Z": fixtureZ, "Beneath": fixtureBeneath, "BeneathKept": fixtureBeneathKept, "Agreeing": fixtureAgreeing, "Meeting": fixtureMeeting,
		"mixins": fixtureMixins, "kept": fixtureKept, "swap": fixtureSwap, "beside": fixtureBeside,
		"three": fixtureThree, "trade": fixtureTrade, "far": fixtureFar, "loser": fixtureLoser,
		"outvoted": fixtureOutvoted, "group": fixtureGroup, "brought": fixtureBrought} {
		t.Run(name, func(t *testing.T) {
			got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(src), true)

			assert.Equal(t, passed(OracleSequenceOrder), got)
		})
	}
}

// TestSequenceOrderInvariant_CompileFailureIsReported covers the error path of
// the second compile the oracle makes, the permuted one, which a correct
// compiler never fails on a source that already compiled.
func TestSequenceOrderInvariant_CompileFailureIsReported(t *testing.T) {
	compiles := 0
	var orig compileFunc
	orig = swapCompile(t, func(ctx context.Context, spec string, data []byte, ps []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		compiles++
		if compiles == 2 {
			return nil, nil, errors.New("boom")
		}
		return orig(ctx, spec, data, ps)
	})

	got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureX), true)

	assert.Equal(t, found(OracleSequenceOrder, "recompile permuted: boom"), got)
	assert.Equal(t, 2, compiles, "the oracle compiles its baseline and the permutation, and no more")
}

// TestSequenceOrderInvariant_DeclinesWhatItCannotPermute pins the oracle's
// declines before it compiles anything: a source that is no YAML, one the
// rewrite cannot permute faithfully, and one whose reversal writes the same
// bytes.
func TestSequenceOrderInvariant_DeclinesWhatItCannotPermute(t *testing.T) {
	swapCompile(t, func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		require.FailNow(t, "a declined source is never compiled")
		return nil, nil, nil
	})
	for _, tc := range []struct {
		name, src string
		why       Decline
	}{
		{"a source that is no YAML", "\t- a: [\n", DeclineNotYAML},
		{"an anchor given twice", schemas("A: &a {type: string}", "B: &a {type: integer}",
			"AB: {allOf: [{title: a}, {title: b}]}"), DeclineAnchorRedefined},
		{"two entries alike", schemas("AB: {allOf: [{title: a}, {title: a}]}"), DeclineNothingToPermute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(tc.src), true)

			assert.Equal(t, declined(OracleSequenceOrder, tc.why), got)
		})
	}
}

// TestSequenceOrderInvariant_AFormPastTheBoundIsDeclined pins that a document
// whose canonical form nests past the harness's bound is declined, not reported
// as the compiler's finding.
func TestSequenceOrderInvariant_AFormPastTheBoundIsDeclined(t *testing.T) {
	deep := ir.RawValue(strings.Repeat("[", maxCanonicalDepth+1) + strings.Repeat("]", maxCanonicalDepth+1))
	plant(t, func(t *testing.T, _ bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
		model, ok := doc.Types[abID].(*ir.Model)
		require.True(t, ok, "the fixture must lower AB to a model")
		model.Unmodeled = ir.Unmodeled{"openapi:x": {Reason: ir.ReasonValidationOnly, Value: deep}}
		return doc, diags, nil
	})

	got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureX), true)

	assert.Equal(t, declined(OracleSequenceOrder, DeclineTooDeep), got)
}

// TestSequenceOrderInvariant_UnmodeledTargetsKeyNormalization pins how narrow
// targetNames is in canon.go: reversing an allOf inside a kept keyword shifts
// its /allOf/N/$ref target keys and passes, while swapping non-reversed oneOf
// target keys or altering a target ID still fails the oracle.
func TestSequenceOrderInvariant_UnmodeledTargetsKeyNormalization(t *testing.T) {
	src := schemas(
		"A: {type: string}",
		"B: {type: integer}",
		"Holder: {type: object, not: {allOf: [{description: alpha, $ref: '#/components/schemas/A'}, {description: beta, $ref: '#/components/schemas/B'}]}}",
	)
	assert.Equal(t, passed(OracleSequenceOrder), sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(src), true))

	t.Run("changed target ID is still caught", func(t *testing.T) {
		plant(t, func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
			if !reversed {
				return doc, diags, nil
			}
			holder := doc.Types["type/openapi/components/schemas/Holder"].Common()
			entry := holder.Unmodeled["openapi:not"]
			entry.Targets["/allOf/0/$ref"] = "type/openapi/components/schemas/A"
			holder.Unmodeled["openapi:not"] = entry
			return doc, diags, nil
		})
		got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(src), true)
		assert.Equal(t, OutcomeSequenceOrderDependent, got.Outcome)
	})

	t.Run("non-reversed oneOf target keys are not erased", func(t *testing.T) {
		oneOfSrc := schemas(
			"Alpha: {type: string}",
			"Beta: {type: integer}",
			"Holder: {allOf: [{properties: {alpha: {type: string}}}, {properties: {beta: {type: string}}}], "+
				"not: {oneOf: [{$ref: '#/components/schemas/Alpha'}, {$ref: '#/components/schemas/Beta'}]}}",
		)
		plant(t, func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
			if !reversed {
				return doc, diags, nil
			}
			holder := doc.Types["type/openapi/components/schemas/Holder"].Common()
			entry := holder.Unmodeled["openapi:not"]
			entry.Targets["/oneOf/0/$ref"], entry.Targets["/oneOf/1/$ref"] = entry.Targets["/oneOf/1/$ref"], entry.Targets["/oneOf/0/$ref"]
			holder.Unmodeled["openapi:not"] = entry
			return doc, diags, nil
		})
		got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(oneOfSrc), true)
		assert.Equal(t, OutcomeSequenceOrderDependent, got.Outcome)
	})
}
