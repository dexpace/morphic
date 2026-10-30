// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// stamped returns doc stamped with this build's IRVersion, so the version check
// draws nothing a test about some other rule has to filter.
func stamped(doc *ir.Document) *ir.Document {
	doc.IRVersion = ir.IRVersion
	return doc
}

// markerCarrier is one place a document stores an inferred-marker list: a
// document holding list there and nothing else wrong, and the path of the list.
type markerCarrier struct {
	name string
	doc  func(list ir.Inferred) *ir.Document
	path string
}

// markerCarriers reach both carrier types, ir.Provenance and ir.TypeRef, under
// Types, Services and Diagnostics, so a check scoped to one carrier or one part
// of the document misses a row.
func markerCarriers() []markerCarrier {
	property := func(ref ir.TypeRef) *ir.Document {
		return closedDoc(&ir.Model{ID: "t/x/M", Name: named("m"), Properties: []ir.Property{{
			ID: "p/x/M/f", Name: named("f"), Type: ref,
		}}})
	}
	return []markerCarrier{
		{"a model's provenance", func(list ir.Inferred) *ir.Document {
			return closedDoc(&ir.Model{ID: "t/x/M", Name: named("m"), Provenance: ir.Provenance{Inferred: list}})
		}, "doc.Types[t/x/M].Provenance.Inferred"},
		{"a property's nullable reference", func(list ir.Inferred) *ir.Document {
			return property(ir.TypeRef{Target: "t/x/Leaf", Nullable: true, Inferred: list})
		}, "doc.Types[t/x/M].Properties[0].Type.Inferred"},
		{"a parameter's nullable reference", func(list ir.Inferred) *ir.Document {
			doc := closedDoc()
			doc.Services = []ir.Service{{ID: "s/x/S", Name: named("s"), Groups: []ir.OperationGroup{{
				ID: "g/x/S/g", Name: named("g"), Operations: []ir.Operation{{
					ID: "op/x/S/op", Name: named("op"), Params: []ir.Parameter{{
						ID: "param/x/S/op/q", Name: named("q"),
						Type: ir.TypeRef{Target: "t/x/Leaf", Nullable: true, Inferred: list},
					}},
				}},
			}}}}
			return doc
		}, "doc.Services[0].Groups[0].Operations[0].Params[0].Type.Inferred"},
		{"a diagnostic's provenance", func(list ir.Inferred) *ir.Document {
			doc := closedDoc()
			doc.Diagnostics = []ir.Diagnostic{{
				Severity: ir.SeverityWarning, Code: "x/finding", Message: "m", Provenance: ir.Provenance{Inferred: list},
			}}
			return doc
		}, "doc.Diagnostics[0].Provenance.Inferred"},
	}
}

// TestVerify_AnInferredMarkerNotInTheJoinedFormIsAViolation is GitHub #1114's
// reproduction on both carrier types, under Types, Services and Diagnostics:
// each list ir.JoinInferred would rewrite is one violation at the list, naming
// what the join writes, and each list it would keep is clean.
func TestVerify_AnInferredMarkerNotInTheJoinedFormIsAViolation(t *testing.T) {
	t.Parallel()
	for _, carrier := range markerCarriers() {
		for _, tc := range []struct {
			list   ir.Inferred
			joined ir.Inferred
		}{
			{"", ""},
			{"a", "a"},
			{"a,b", "a,b"},
			{"a,,b", "a,b"},
			{",a", "a"},
			{"a,a", "a"},
			{"b,a,b", "b,a"},
			{",", ""},
		} {
			t.Run(carrier.name+"/"+string(tc.list), func(t *testing.T) {
				t.Parallel()
				got := verifyDeclared(stamped(carrier.doc(tc.list)))
				if tc.list == tc.joined {
					assert.Empty(t, got)
					return
				}
				assert.Equal(t, []irverify.Violation{{
					Code:    "ir/inferred-not-canonical",
					Message: `inferred marker "` + string(tc.list) + `" is not the list "` + string(tc.joined) + `" ir.JoinInferred writes for it`,
					Path:    carrier.path,
				}}, got)
			})
		}
	}
}

// TestVerify_AnInferredMarkerAnswersToEachRuleItBreaks holds the marker's
// rules apart: a list is read byte by byte, so bytes that do not decode are
// checkUTF8's alone unless they repeat, and a list on a reference admitting no
// null breaks that reference's rule beside its own.
func TestVerify_AnInferredMarkerAnswersToEachRuleItBreaks(t *testing.T) {
	t.Parallel()
	const at = "doc.Types[t/x/M].Properties[0].Type.Inferred"
	for _, tc := range []struct {
		name  string
		ref   ir.TypeRef
		codes []string
	}{
		{"a name that does not decode", ir.TypeRef{Nullable: true, Inferred: "caf\xe9"},
			[]string{"ir/invalid-utf8"}},
		{"two names whose bytes differ, though neither decodes", ir.TypeRef{Nullable: true, Inferred: "\xe9,\xe8"},
			[]string{"ir/invalid-utf8"}},
		{"a name that does not decode, twice", ir.TypeRef{Nullable: true, Inferred: "caf\xe9,caf\xe9"},
			[]string{"ir/inferred-not-canonical", "ir/invalid-utf8"}},
		{"a repeat on a reference admitting no null", ir.TypeRef{Inferred: "a,a"},
			[]string{"ir/inferred-not-canonical", "ir/type-ref-inferred-not-nullable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.ref.Target = "t/x/Leaf"
			got := verifyDeclared(stamped(closedDoc(&ir.Model{ID: "t/x/M", Name: named("m"), Properties: []ir.Property{{
				ID: "p/x/M/f", Name: named("f"), Type: tc.ref,
			}}})))
			require.Equal(t, tc.codes, violationCodes(got))
			for _, v := range got {
				assert.Equal(t, at, v.Path, v.Code)
			}
		})
	}
}

// TestVerify_OnlyAnInferredMarkerIsHeldToTheListForm holds the check to the
// type it reads by: a document's other strings may spell anything, so a
// summary, a tag and a wire name that would be broken lists draw nothing.
func TestVerify_OnlyAnInferredMarkerIsHeldToTheListForm(t *testing.T) {
	t.Parallel()
	model := &ir.Model{ID: "t/x/M", Name: named("m"), Docs: ir.Docs{Summary: "a,,b"}, Properties: []ir.Property{{
		ID: "p/x/M/f", Name: named("f"), WireName: "a,a", Type: ir.TypeRef{Target: "t/x/Leaf"},
	}}}
	model.Tags = []string{",a"}
	doc := stamped(closedDoc(model))
	assert.Empty(t, verifyDeclared(doc))
}
