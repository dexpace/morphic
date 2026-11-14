// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
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
func sequenceVerdict(t *testing.T, r Result) Verdict {
	t.Helper()
	for _, v := range r.Verdicts {
		if v.Oracle == OracleSequenceOrder {
			return v
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
			codes: map[string]string{"openapi/degraded-construct": s + "/AB/allOf/1/properties/p"},
			want:  passed(OracleSequenceOrder),
		},
		{
			name: "a redeclared property whose types disagree",
			src: schemas("AB: {allOf: [{properties: {p: {type: string}, q: {type: object, properties: {x: {type: string}}}}}, " +
				"{properties: {p: {type: integer}, q: {type: object, properties: {y: {type: string}}}}}]}"),
			codes: map[string]string{"openapi/conflicting-redeclaration": s + "/AB/allOf/1/properties/p"},
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
			name: "identical inline declarations in two branches",
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
			// The kept branch is named by its index, which reversing moves.
			name:  "a false branch kept under a name carrying its index",
			src:   schemas("Never: {allOf: [{type: object, properties: {a: {type: string}}}, false]}"),
			kept:  []string{`"openapi:allOf/1"`},
			codes: map[string]string{"openapi/false-schema": s + "/Never/allOf/1"},
			want:  passed(OracleSequenceOrder),
		},
		{
			// Two members whose names are one once their indices are erased.
			name: "two branches kept under names that differ by index alone",
			src:  schemas("Inline: {allOf: [{type: string}, {type: integer}]}"),
			kept: []string{`"openapi:allOf/0"`, `"openapi:allOf/1"`},
			want: passed(OracleSequenceOrder),
		},
		{
			// A reference names the first branch, so the sequence is left as it
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
			// The deepest chain the permutation admits: its canonical form
			// nests past one source bound.
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

// TestCheck_SequenceOrderOracleDeclinesASpecOfTwoFiles pins that a source
// whose compile reads another file is declined: that file names the first
// branch of N's allOf by its index, which the walk does not read, so reversing
// the branches would change what the reference names, not only their order.
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

// diagnosticsAt is the pointers of the diagnostics of one code.
func diagnosticsAt(diags []ir.Diagnostic, code string) []string {
	var out []string
	for _, d := range diags {
		if d.Code == code {
			out = append(out, string(d.Provenance.Pointer))
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
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(ctx context.Context, spec string, data []byte, ps []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		doc, diags, err := orig(ctx, spec, data, ps)
		if err != nil {
			return doc, diags, err
		}
		reversed := bytes.Index(data, []byte("beta")) < bytes.Index(data, []byte("alpha"))
		return edit(t, reversed, doc, diags)
	}
}

// Fixtures of the detection rows, each passing the oracle as the real
// compiler lowers it.
var (
	// fixtureX: two branches declaring different members.
	fixtureX = schemas("AB: {allOf: [{type: object, properties: {alpha: {type: string}}}, " +
		"{type: object, properties: {beta: {type: integer}}}]}")
	// fixtureY: both branches declare p, the same way.
	fixtureY = schemas("AB: {allOf: [{type: object, properties: {p: {type: string}, alpha: {type: string}}}, " +
		"{type: object, properties: {p: {type: string}, beta: {type: integer}}}]}")
	// fixtureMixins: two named conjuncts, which the model composes as mixins.
	fixtureMixins = schemas("AB: {allOf: [{$ref: '#/components/schemas/alpha'}, {$ref: '#/components/schemas/beta'}]}",
		"alpha: {type: object, properties: {x: {type: string}}}", "beta: {type: object, properties: {y: {type: string}}}")
	// fixtureKept: an allOf the model keeps verbatim, under not.
	fixtureKept = schemas("AB: {type: object, not: {allOf: [{required: [alpha]}, {required: [beta]}]}}")
	// fixtureZ: both branches declare p, as an object of their own.
	fixtureZ = schemas("AB: {allOf: [{type: object, properties: {p: {type: object, properties: {x: {type: string}}}, alpha: {type: string}}}, " +
		"{type: object, properties: {p: {type: object, properties: {y: {type: string}}}, beta: {type: string}}}]}")
)

// abID is the model every detection fixture declares.
const abID ir.TypeID = "t/openapi/components/schemas/AB"

// property is the member of AB with the wire name.
func property(t *testing.T, doc *ir.Document, wire string) *ir.Property {
	t.Helper()
	m, ok := doc.Types[abID].(*ir.Model)
	require.True(t, ok, "the fixture must lower AB to a model")
	for i := range m.Properties {
		if m.Properties[i].WireName == wire {
			return &m.Properties[i]
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
				property(t, doc, wire).Type.Target = "t/prim/integer"
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
				property(t, doc, "p").Type.Target = "t/anon/components/schemas/AB/allOf/0/properties/p"
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
					diagnostic(ir.SeverityInfo, "x/node", "", "t/anon/components/schemas/AB/allOf/0"))
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
				m, ok := doc.Types[abID].(*ir.Model)
				require.True(t, ok, "the fixture must lower AB to a model")
				require.Len(t, m.Mixins, 2, "the fixture must compose two mixins")
				if reversed {
					m.Mixins = append(m.Mixins, m.Mixins[0])
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order"},
		},
		{
			// A kept allOf compares as a multiset of its branches.
			name: "a branch of a kept allOf listed twice in one order",
			src:  fixtureKept,
			edit: func(t *testing.T, reversed bool, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic, error) {
				m, ok := doc.Types[abID].(*ir.Model)
				require.True(t, ok, "the fixture must lower AB to a model")
				entry, kept := m.Unmodeled["openapi:not"]
				require.True(t, kept, "the fixture must keep not verbatim")
				if reversed {
					entry.Value = ir.RawValue(`{"allOf":[{"required":["beta"]},{"required":["alpha"]},{"required":["alpha"]}]}`)
					m.Unmodeled = ir.Unmodeled{"openapi:not": entry}
				}
				return doc, diags, nil
			},
			found:    true,
			contains: []string{"document depends on sequence order"},
		},
		{
			// The two reports differ in which member they name, not only in
			// which branch, so they are no tie.
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

			got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(tc.src), true)

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
	for name, src := range map[string]string{"X": fixtureX, "Y": fixtureY, "Z": fixtureZ,
		"mixins": fixtureMixins, "kept": fixtureKept} {
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
	orig := compile
	t.Cleanup(func() { compile = orig })
	compiles := 0
	compile = func(ctx context.Context, spec string, data []byte, ps []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		compiles++
		if compiles == 2 {
			return nil, nil, errors.New("boom")
		}
		return orig(ctx, spec, data, ps)
	}

	got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureX), true)

	assert.Equal(t, found(OracleSequenceOrder, "recompile permuted: boom"), got)
	assert.Equal(t, 2, compiles, "the oracle compiles its baseline and the permutation, and no more")
}

// TestSequenceOrderInvariant_DeclinesWhatItCannotPermute pins the oracle's
// declines before it compiles anything: a source that is no YAML, one the
// rewrite cannot permute faithfully, and one whose reversal writes the same
// bytes.
func TestSequenceOrderInvariant_DeclinesWhatItCannotPermute(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })
	compile = func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		require.FailNow(t, "a declined source is never compiled")
		return nil, nil, nil
	}
	for _, tc := range []struct {
		name, src string
		why       Decline
	}{
		{"a source that is no YAML", "\t- a: [\n", DeclineNotYAML},
		{"an anchor given twice", schemas("A: &a {type: string}", "B: &a {type: integer}",
			"AB: {allOf: [{title: a}, {title: b}]}"), DeclineAnchorRedefined},
		{"two branches alike", schemas("AB: {allOf: [{title: a}, {title: a}]}"), DeclineNothingToPermute},
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
		m, ok := doc.Types[abID].(*ir.Model)
		require.True(t, ok, "the fixture must lower AB to a model")
		m.Unmodeled = ir.Unmodeled{"openapi:x": {Reason: ir.ReasonValidationOnly, Value: deep}}
		return doc, diags, nil
	})

	got := sequenceOrderInvariant(context.Background(), "spec.yaml", []byte(fixtureX), true)

	assert.Equal(t, declined(OracleSequenceOrder, DeclineTooDeep), got)
}
