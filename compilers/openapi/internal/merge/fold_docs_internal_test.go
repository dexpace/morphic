// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestFold_DifferingDescriptionsKeepTheLater pins a merge outcome
// that is reported without being a conflict: two entries describing the same
// field differently cannot both be kept, so the later wins, the loss is
// announced rather than silent, and the losing declaration is kept whole.
func TestFold_DifferingDescriptionsKeepTheLater(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Docs: ir.Docs{Description: "first"}, Provenance: ptrAt("/first")}

	dst = foldTwo(stubbed, dst, ir.Property{WireName: "id", Docs: ir.Docs{Description: "second"}, Provenance: ptrAt("/other")},
		unread(t))

	assert.Equal(t, "second", dst.Docs.Description)
	require.Len(t, *recorded, 1)
	assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
	assert.Equal(t, jsontext.Pointer("/first"), (*recorded)[0].Provenance.Pointer, "at the declaration that lost")
	assert.Contains(t, (*recorded)[0].Message, `"id"`)
	assert.Contains(t, (*recorded)[0].Message, "description")
	assert.Equal(t, ir.RawValue(firstSource), dst.Unmodeled[losingDeclarationKey+"/first"].Value)
}

// TestFold_AnIdenticalDescriptionIsNotADisagreement pins the other
// side of that check: repeating the same description in both entries is one
// construct written twice, not two that disagree.
func TestFold_AnIdenticalDescriptionIsNotADisagreement(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Docs: ir.Docs{Description: "same"}}

	dst = foldTwo(stubbed, dst, ir.Property{WireName: "id", Docs: ir.Docs{Description: "same"}, Provenance: ptrAt("/other")}, unread(t))

	assert.Equal(t, "same", dst.Docs.Description)
	assert.Empty(t, *recorded)
}

// TestFold_TitleAndExternalDocsFoldLikeDescription pins that a
// summary and external links fold as a description does: an empty side adopts
// the other, equal sides are one construct written twice, and differing sides
// keep the later and the earlier declaration whole, in either declaration
// order.
func TestFold_TitleAndExternalDocsFoldLikeDescription(t *testing.T) {
	t.Parallel()
	linkA := []ir.Link{{URL: "https://a.example"}}
	linkB := []ir.Link{{URL: "https://b.example"}}
	summary := func(s string) ir.Docs { return ir.Docs{Summary: s} }
	links := func(links []ir.Link) ir.Docs { return ir.Docs{ExternalDocs: links} }

	tests := []struct {
		name     string
		dst, src ir.Docs
		want     ir.Docs
		detail   string // empty when nothing is lost
	}{
		{"summary, dst empty", summary(""), summary("B"), summary("B"), ""},
		{"summary, src empty", summary("A"), summary(""), summary("A"), ""},
		{"summary, equal", summary("A"), summary("A"), summary("A"), ""},
		{"summary, different", summary("A"), summary("B"), summary("B"), "title"},
		{"summary, different swapped", summary("B"), summary("A"), summary("A"), "title"},
		{"external docs, dst empty", links(nil), links(linkB), links(linkB), ""},
		{"external docs, src empty", links(linkA), links(nil), links(linkA), ""},
		{"external docs, equal", links(linkA), links(linkA), links(linkA), ""},
		{"external docs, different", links(linkA), links(linkB), links(linkB), "external docs"},
		{"external docs, different swapped", links(linkB), links(linkA), links(linkA), "external docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stubbed, recorded := stubMerger(nil)
			dst := ir.Property{WireName: "id", Docs: tt.dst, Provenance: ptrAt("/first")}
			src := ir.Property{WireName: "id", Docs: tt.src, Provenance: ptrAt("/other")}

			dst = foldTwo(stubbed, dst, src, unread(t))

			assert.Empty(t, cmp.Diff(tt.want, dst.Docs))
			if tt.detail == "" {
				assert.Empty(t, *recorded)
				assert.Empty(t, dst.Unmodeled)
				return
			}
			require.Len(t, *recorded, 1)
			assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity)
			assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			assert.Equal(t, jsontext.Pointer("/first"), (*recorded)[0].Provenance.Pointer)
			assert.Contains(t, (*recorded)[0].Message, `"id"`)
			assert.Contains(t, (*recorded)[0].Message, tt.detail)
			require.Len(t, dst.Unmodeled, 1)
			entry, ok := dst.Unmodeled[losingDeclarationKey+"/first"]
			require.True(t, ok)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Equal(t, ir.RawValue(firstSource), entry.Value)
		})
	}
}

// TestFold_EveryDocsDisagreementIsReported pins that foldDocs
// judges each docs field rather than stopping at the first that differs: a
// redeclaration giving all three differently is reported three times, and
// kept once.
func TestFold_EveryDocsDisagreementIsReported(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(nil)
	later := ir.Docs{Summary: "B", Description: "dB", ExternalDocs: []ir.Link{{URL: "https://b.example"}}}
	dst := ir.Property{WireName: "id", Provenance: ptrAt("/first"),
		Docs: ir.Docs{Summary: "A", Description: "dA", ExternalDocs: []ir.Link{{URL: "https://a.example"}}}}
	src := ir.Property{WireName: "id", Provenance: ptrAt("/other"), Docs: later}

	dst = foldTwo(stubbed, dst, src, unread(t))

	assert.Empty(t, cmp.Diff(later, dst.Docs))
	require.Len(t, *recorded, 3)
	for i, detail := range []string{"title", "description", "external docs"} {
		assert.Contains(t, (*recorded)[i].Message, detail)
	}
	require.Len(t, dst.Unmodeled, 1)
	assert.Equal(t, ir.RawValue(firstSource), dst.Unmodeled[losingDeclarationKey+"/first"].Value)
}

// TestFold_ADeprecationTheFieldTakesIsACopy pins that the field holds the
// deprecation the fold keeps apart from the declaration giving it, whose own a
// later stage writes (copyOf), and none where no declaration gives one.
func TestFold_ADeprecationTheFieldTakesIsACopy(t *testing.T) {
	t.Parallel()
	stubbed, _ := stubMerger(nil)
	given := &ir.Deprecation{Message: "m", ReplacedBy: []ir.EntityRef{{Type: "type/r"}}}
	field := foldTwo(stubbed, ir.Property{WireName: "p", Deprecation: given}, ir.Property{WireName: "p", Provenance: ptrAt("/other")}, unread(t))
	assert.Equal(t, given, field.Deprecation)
	assert.NotSame(t, given, field.Deprecation)
	assert.Nil(t, foldTwo(stubbed, ir.Property{WireName: "p"}, ir.Property{WireName: "p", Provenance: ptrAt("/other")}, unread(t)).Deprecation)
}

// TestFold_ADeclarationHoldingNotesOffItsNodeIsReported pins the check on what
// a fold reads: a declaration the walk gave its schema's notes off the node that
// schema built, the model's own or one an entry brings, judged or past the
// bound, is reported as a compiler bug where its declaration is, and the note
// it holds is the field's all the same. One holding none draws nothing.
func TestFold_ADeclarationHoldingNotesOffItsNodeIsReported(t *testing.T) {
	t.Parallel()
	const own, brought ir.PropertyID = "property/own", "property/brought"
	decls := func() []declaration {
		return []declaration{
			{property: ir.Property{ID: own, WireName: "p", Type: primitiveRef(ir.PrimitiveString), Docs: ir.Docs{Description: "d"}, Provenance: ptrAt("/own")},
				source: written(`{}`)},
			{property: ir.Property{ID: brought, WireName: "p", Type: primitiveRef(ir.PrimitiveString), Provenance: ptrAt("/part/p")},
				via: &composedEntry{target: "type/part", at: "/M/allOf/0"}, typeAt: sitesAt("/part/p")},
		}
	}
	for _, tc := range []struct {
		name    string
		carried map[ir.PropertyID]bool
		at      []jsontext.Pointer
	}{
		{"none", nil, []jsontext.Pointer{}},
		{"another property", map[ir.PropertyID]bool{"property/other": true}, []jsontext.Pointer{}},
		{"the model's own", map[ir.PropertyID]bool{own: true}, []jsontext.Pointer{"/own"}},
		{"one an entry brings", map[ir.PropertyID]bool{brought: true}, []jsontext.Pointer{"/M/allOf/0"}},
		{"both", map[ir.PropertyID]bool{own: true, brought: true}, []jsontext.Pointer{"/own", "/M/allOf/0"}},
	} {
		stubbed, recorded := stubMerger(narrowingRegistry())
		stubbed.Carried = tc.carried
		field := stubbed.fold(decls())
		assert.Equal(t, "d", field.Docs.Description, tc.name)
		at := make([]jsontext.Pointer, 0, len(*recorded))
		for _, diagnostic := range *recorded {
			assert.Equal(t, diag.InternalInvariant, diagnostic.Code, tc.name)
			assert.Equal(t, ir.SeverityError, diagnostic.Severity, tc.name)
			assert.Contains(t, diagnostic.Message, `a declaration of field "p" a composition folds holds notes off the node its schema built`, tc.name)
			at = append(at, diagnostic.Provenance.Pointer)
		}
		assert.Equal(t, tc.at, at, tc.name)
	}

	stubbed, recorded := stubMerger(narrowingRegistry())
	stubbed.Carried = map[ir.PropertyID]bool{"property/last": true}
	many := make([]declaration, maxFoldDeclarations+1)
	for i := range many {
		many[i] = decls()[0]
	}
	many[maxFoldDeclarations].property.ID = "property/last"
	stubbed.fold(many)
	assert.True(t, slices.ContainsFunc(*recorded, func(diagnostic ir.Diagnostic) bool { return diagnostic.Code == diag.InternalInvariant }), "past the bound")
}
