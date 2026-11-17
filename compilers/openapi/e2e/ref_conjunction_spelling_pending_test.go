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

// fourSpellings are the four ways a schema writes keywords k beside a $ref to
// T: beside it, beside an allOf of it, on its allOf entry, and on an entry of
// their own.
func fourSpellings(k string) map[string]string {
	ref := "$ref: '#/components/schemas/T'"
	return map[string]string{
		"beside the $ref":  "{" + ref + ", " + k + "}",
		"beside an allOf":  "{allOf: [{" + ref + "}], " + k + "}",
		"on its entry":     "{allOf: [{" + ref + ", " + k + "}]}",
		"on an entry next": "{allOf: [{" + ref + "}, {" + k + "}]}",
	}
}

// requireOneDocument holds the four spellings of k beside a $ref to the
// target, at each of the sites given, to one document up to the entries'
// pointers.
func requireOneDocument(t *testing.T, sites []conjSite, target, k string) {
	t.Helper()
	spellings := fourSpellings(k)
	for _, site := range sites {
		want := writtenAnyway(t, site, target, spellings["beside the $ref"])
		for name, schema := range spellings {
			got := writtenAnyway(t, site, target, schema)
			assert.Empty(t, cmp.Diff(want, got, cmp.AllowUnexported(entryWritten{})), "%s, %s", site.name, name)
		}
	}
}

// Pending: the election reads a conjunct's value keywords together, so an
// entry writing a const and an enum sharing no value, or an enum beside a
// $ref naming no schema, states no value set, and the allOf composes a Model
// over a string where the value set beside the $ref or the allOf lowers as
// the const, or the enum, keeping the $ref.

// TestRefConjunction_AnEntrysValueSetLowersAsBesideItsRef holds the four
// spellings of a value set the election of competing keywords decides beside
// a $ref to one document, at every site.
func TestRefConjunction_AnEntrysValueSetLowersAsBesideItsRef(t *testing.T) {
	t.Parallel()
	for _, tc := range passedOverRefs {
		requireOneDocument(t, conjSites, tc.target, tc.keywords)
	}
}

// Pending: an inline conjunct an allOf lowers from stands at its own pointer,
// behind an alias at a component and naming the carrier's $ref at a property,
// where beside the $ref or the allOf the position owns it.

// TestRefConjunction_AUnionBesideARefToTheTopTypeStandsAtThePosition holds
// the four spellings of a union beside a $ref to a schema admitting every
// kind to one document, the union the node the position owns.
func TestRefConjunction_AUnionBesideARefToTheTopTypeStandsAtThePosition(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"T: {}", "T: true", "T: {properties: {a: {type: string}}}"} {
		requireOneDocument(t, conjSites, target, "oneOf: [{type: string}, {type: integer}]")
	}
}

// Pending: a component written as a $ref to a discriminated schema, saying
// nothing else, is the Model over it with no tag, where an allOf of the $ref
// is the subtype the discriminator routes to by the component's name.

// TestRefConjunction_AComponentOverADiscriminatedBaseIsTaggedInEverySpelling
// holds the tag a component over a discriminated schema carries to one value
// in every spelling of a use saying nothing more of it.
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

// Pending: a redaction beside a $ref to an object at a carrier is Sensitive on
// the Model the use owns, which no emitter collapses, where beside a $ref to a
// string it is the carrier's Secret.

// TestRefConjunction_ARedactionBesideARefToAnObjectIsTheCarriersInEverySpelling
// holds format: password beside a $ref to an object, at a property, to the
// property's Secret in every spelling, its type the target.
func TestRefConjunction_ARedactionBesideARefToAnObjectIsTheCarriersInEverySpelling(t *testing.T) {
	t.Parallel()
	for name, schema := range fourSpellings("format: password") {
		doc, _ := compileUnrefused(t, conjSites[1].write("3.1.0", schema, conjTargets["Obj"]))
		holder, ok := kindNode(t, doc, componentID("Holder")).(*ir.Model)
		require.True(t, ok, name)
		require.Len(t, holder.Properties, 1, name)
		assert.True(t, holder.Properties[0].Secret, "%s: the carrier is redacted", name)
		assert.Equal(t, componentID("Obj"), holder.Properties[0].Type.Target, "%s: and names the target", name)
	}
}
