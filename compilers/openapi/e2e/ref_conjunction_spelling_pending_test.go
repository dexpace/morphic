// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

//go:build pending

package e2e_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// TestRefConjunction_AValueSetBesideAFailedRefLowersAsBesideItsRef holds an
// enum beside a $ref naming no schema to one document in each spelling but an
// entry writing both, which is read whole. Pending (GitHub #2064): an enum on
// an entry of its own next to the $ref composes a Model, where beside the $ref
// or the allOf it lowers as the enum keeping the $ref.
func TestRefConjunction_AValueSetBesideAFailedRefLowersAsBesideItsRef(t *testing.T) {
	t.Parallel()
	const target, k = "Other: {type: string}", "enum: [a]"
	spellings := fourSpellings(k)
	for _, site := range conjSites {
		want := writtenAnyway(t, site, target, spellings["beside the $ref"])
		for _, name := range []string{"beside an allOf", "on an entry next"} {
			got := writtenAnyway(t, site, target, spellings[name])
			assert.Empty(t, cmp.Diff(want, got, cmp.AllowUnexported(entryWritten{})), "%s, %s", site.name, name)
		}
	}
}

// TestRefConjunction_AComponentOverADiscriminatedBaseIsTaggedInEverySpelling
// holds the tag a component over a discriminated schema carries to one value
// in every spelling of a use saying nothing more of it. Pending (GitHub
// #2060): a component written as a $ref to a discriminated schema is the
// Model over it with no tag, where an allOf of the $ref is the subtype the
// discriminator routes to by the component's name.
func TestRefConjunction_AComponentOverADiscriminatedBaseIsTaggedInEverySpelling(t *testing.T) {
	t.Parallel()
	disc := "T: {type: object, required: [kind], properties: {kind: {type: string}}, discriminator: {propertyName: kind}}"
	tags := map[string]string{}
	for name, schema := range fourSpellings("$comment: c") {
		doc, _ := compileUnrefused(t, conjSites[0].write("3.1.0", schema, disc))
		m, ok := kindNode(t, doc, componentID("Use")).(*ir.Model)
		require.True(t, ok, name)
		tags[name] = m.DiscriminatorValue
	}
	for name, tag := range tags {
		assert.Equal(t, tags["beside the $ref"], tag, name)
	}
}

// TestRefConjunction_ARedactionBesideARefToAnObjectIsTheCarriersInEverySpelling
// holds format: password beside a $ref to an object, at a property, to the
// property's Secret in every spelling, its type the target. Pending (GitHub
// #2061): a redaction beside a $ref to an object at a carrier is Sensitive on
// the Model the use owns, which no emitter collapses, where beside a $ref to a
// string it is the carrier's Secret.
func TestRefConjunction_ARedactionBesideARefToAnObjectIsTheCarriersInEverySpelling(t *testing.T) {
	t.Parallel()
	for name, schema := range fourSpellings("format: password") {
		doc, _ := compileUnrefused(t, conjSites[1].write("3.1.0", schema, "T: {type: object, properties: {a: {type: string}}}"))
		holder, ok := kindNode(t, doc, componentID("Holder")).(*ir.Model)
		require.True(t, ok, name)
		require.Len(t, holder.Properties, 1, name)
		assert.True(t, holder.Properties[0].Secret, "%s: the carrier is redacted", name)
		assert.Equal(t, componentID("T"), holder.Properties[0].Type.Target, "%s: and names the target", name)
	}
}
