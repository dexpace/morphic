// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestFold_DifferingDescriptionsKeepTheFirst pins a merge outcome
// that is reported without being a conflict: two branches describing the same
// field differently cannot both be kept, so the first wins, the loss is
// announced rather than silent, and the losing declaration is kept whole.
func TestFold_DifferingDescriptionsKeepTheFirst(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Docs: ir.Docs{Description: "first"}}

	dst = foldTwo(g, dst, ir.Property{WireName: "id", Docs: ir.Docs{Description: "second"}, Provenance: ptrAt("/other")},
		written(`{"description":"second"}`))

	assert.Equal(t, "first", dst.Docs.Description)
	require.Len(t, *recorded, 1)
	assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
	assert.Contains(t, (*recorded)[0].Message, `"id"`)
	assert.Contains(t, (*recorded)[0].Message, "description")
	assert.Equal(t, ir.RawValue(`{"description":"second"}`), dst.Unmodeled[losingDeclarationKey+"/other"].Value)
}

// TestFold_AnIdenticalDescriptionIsNotADisagreement pins the other
// side of that check: repeating the same description in both branches is one
// construct written twice, not two that disagree.
func TestFold_AnIdenticalDescriptionIsNotADisagreement(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	dst := ir.Property{WireName: "id", Docs: ir.Docs{Description: "same"}}

	dst = foldTwo(g, dst, ir.Property{WireName: "id", Docs: ir.Docs{Description: "same"}, Provenance: ptrAt("/other")}, unread(t))

	assert.Equal(t, "same", dst.Docs.Description)
	assert.Empty(t, *recorded)
}

// TestFold_TitleAndExternalDocsFoldLikeDescription pins that a
// summary and external links fold as a description does: an empty side adopts
// the other, equal sides are one construct written twice, and differing sides
// keep the first and the redeclaration whole, in either declaration order.
func TestFold_TitleAndExternalDocsFoldLikeDescription(t *testing.T) {
	t.Parallel()
	linkA := []ir.Link{{URL: "https://a.example"}}
	linkB := []ir.Link{{URL: "https://b.example"}}
	summary := func(s string) ir.Docs { return ir.Docs{Summary: s} }
	links := func(l []ir.Link) ir.Docs { return ir.Docs{ExternalDocs: l} }

	tests := []struct {
		name     string
		dst, src ir.Docs
		want     ir.Docs
		detail   string // empty when nothing is lost
	}{
		{"summary, dst empty", summary(""), summary("B"), summary("B"), ""},
		{"summary, src empty", summary("A"), summary(""), summary("A"), ""},
		{"summary, equal", summary("A"), summary("A"), summary("A"), ""},
		{"summary, different", summary("A"), summary("B"), summary("A"), "title"},
		{"summary, different swapped", summary("B"), summary("A"), summary("B"), "title"},
		{"external docs, dst empty", links(nil), links(linkB), links(linkB), ""},
		{"external docs, src empty", links(linkA), links(nil), links(linkA), ""},
		{"external docs, equal", links(linkA), links(linkA), links(linkA), ""},
		{"external docs, different", links(linkA), links(linkB), links(linkA), "external docs"},
		{"external docs, different swapped", links(linkB), links(linkA), links(linkB), "external docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, recorded := stubMerger(nil)
			dst := ir.Property{WireName: "id", Docs: tt.dst}
			src := ir.Property{WireName: "id", Docs: tt.src, Provenance: ptrAt("/other")}
			source := unread(t)
			if tt.detail != "" {
				source = written(`{"x":1}`)
			}

			dst = foldTwo(g, dst, src, source)

			assert.Empty(t, cmp.Diff(tt.want, dst.Docs))
			if tt.detail == "" {
				assert.Empty(t, *recorded)
				assert.Empty(t, dst.Unmodeled)
				return
			}
			require.Len(t, *recorded, 1)
			assert.Equal(t, ir.SeverityInfo, (*recorded)[0].Severity)
			assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			assert.Equal(t, jsontext.Pointer("/other"), (*recorded)[0].Provenance.Pointer)
			assert.Contains(t, (*recorded)[0].Message, `"id"`)
			assert.Contains(t, (*recorded)[0].Message, tt.detail)
			require.Len(t, dst.Unmodeled, 1)
			entry, ok := dst.Unmodeled[losingDeclarationKey+"/other"]
			require.True(t, ok)
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Equal(t, ir.RawValue(`{"x":1}`), entry.Value)
		})
	}
}

// TestFold_EveryDocsDisagreementIsReported pins that foldDocs
// judges each docs field rather than stopping at the first that differs: a
// redeclaration giving all three differently is reported three times, and
// kept once.
func TestFold_EveryDocsDisagreementIsReported(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	first := ir.Docs{Summary: "A", Description: "dA", ExternalDocs: []ir.Link{{URL: "https://a.example"}}}
	dst := ir.Property{WireName: "id", Docs: first}
	src := ir.Property{WireName: "id", Provenance: ptrAt("/other"),
		Docs: ir.Docs{Summary: "B", Description: "dB", ExternalDocs: []ir.Link{{URL: "https://b.example"}}}}

	dst = foldTwo(g, dst, src, written(`{"x":1}`))

	assert.Empty(t, cmp.Diff(first, dst.Docs))
	require.Len(t, *recorded, 3)
	for i, detail := range []string{"title", "description", "external docs"} {
		assert.Contains(t, (*recorded)[i].Message, detail)
	}
	require.Len(t, dst.Unmodeled, 1)
	assert.Equal(t, ir.RawValue(`{"x":1}`), dst.Unmodeled[losingDeclarationKey+"/other"].Value)
}
