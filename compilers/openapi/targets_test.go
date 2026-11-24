// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"context"
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestAttachUnmodeledTargets_MatchesEveryEntryForm pins attachUnmodeledTargets
// across empty landings, dead landings, string entries, object/array trees,
// discriminator routes, residue objects without the original ref, collapsed
// allOf provenance, and non-matching payloads.
func TestAttachUnmodeledTargets_MatchesEveryEntryForm(t *testing.T) {
	t.Parallel()
	const (
		subID   ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Sub"
		discID  ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Disc"
		defID   ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Def"
		otherID ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Other"
		deadID  ir.TypeID = "type/anonymous/not-in-registry"
	)

	// Fast paths: nil landings and landings naming no live type return true.
	doc := &ir.Document{Types: ir.TypeRegistry{rootModelID: rootModel()}}
	assert.True(t, openapi.AttachUnmodeledTargets(t.Context(), doc, nil))
	assert.True(t, openapi.AttachUnmodeledTargets(t.Context(), doc, map[schema.LandingSite][]schema.Landing{
		{Source: 0, Pointer: "/components/schemas/Root/not"}: {{ID: deadID, Path: "/$ref", Ref: "#/dead"}},
	}))

	root := rootModel()
	root.Unmodeled = ir.Unmodeled{
		"openapi:empty":         {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`   `), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/x-empty"}},
		"openapi:num":           {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`42`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/x-num"}},
		"openapi:other-src":     {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/sub"}`), Provenance: ir.Provenance{Source: 1, Pointer: "/components/schemas/Root/not"}},
		"openapi:plain-obj":     {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"k":"v"}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/x-obj"}},
		"openapi:unrelated-str": {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`"plain"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/not/x-deep"}},
		"openapi:longer-base":   {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`"plain"`), Provenance: ir.Provenance{Source: 0, Pointer: "/z/deep/path/past/every/landing/pointer"}},
		"openapi:bad-str":       {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"unterminated`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/0/$ref"}},
		"openapi:bad-obj":       {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/not"}},
		"openapi:$ref":          {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"#/sub"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/0/$ref"}},
		"openapi:$dynamicRef":   {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"#/sub"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"}},
		"openapi:exact-str":     {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"#/sub"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/0"}},
		"openapi:miss-str":      {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"#/disc"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"}},
		"openapi:not":           {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/sub"}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/not"}},
		"openapi:residue":       {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`{"not":{"$ref":"#/sub"}}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/0"}},
		"openapi:arr":           {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`[{"$ref":"#/sub"}]`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/oneOf"}},
		"openapi:schema/not":    {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/sub"}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"}},
		"openapi:if-then-else": {
			Reason:     ir.ReasonValidationOnly,
			Value:      ir.RawValue(`{"if":{"$ref":"#/sub"},"then":{"const":{"mapping":{"d":"#/disc"}}}}`),
			Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"},
		},
		"openapi:contains": {
			Reason:     ir.ReasonValidationOnly,
			Value:      ir.RawValue(`{"contains":{"$ref":"#/sub"}}`),
			Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"},
		},
		"openapi:discriminator": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"propertyName":"k","mapping":{"d":"#/disc"},"defaultMapping":"#/def"}`),
			Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"},
		},
		"openapi:disc-bad-mapping": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"propertyName":"k","mapping":"not-an-object"}`),
			Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/1/discriminator"},
		},
		"openapi:disc-wrong-ref": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"propertyName":"k","mapping":{"d":"#/wrong"},"defaultMapping":"#/wrong"}`),
			Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/1/discriminator"},
		},
		"openapi:root-collapsed": {
			Reason:     ir.ReasonValidationOnly,
			Value:      ir.RawValue(`{"$ref":"#/sub"}`),
			Provenance: ir.Provenance{Source: 0, Pointer: ""},
		},
	}

	doc = &ir.Document{
		Types: ir.TypeRegistry{
			rootModelID: root,
			subID:       anonModel(subID),
			discID:      anonModel(discID),
			defID:       anonModel(defID),
			otherID:     anonModel(otherID),
		},
	}

	landings := map[schema.LandingSite][]schema.Landing{
		{Source: 0, Pointer: "/components/schemas/Root/allOf/0"}:                    {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/0/not"}:                {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/0/allOf/1/if"}:         {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/0/contains"}:           {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/bad/not"}:              {{ID: otherID, Path: "/$ref", Ref: "#/other"}},
		{Source: 0, Pointer: "/components/schemas/Root/not"}:                        {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/oneOf/0"}:                    {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/1/not"}:                {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/1"}:                    {{ID: discID, Path: "/mapping/d", Ref: "#/disc", Discriminator: true}},
		{Source: 0, Pointer: "/components/schemas/Root/properties/z/discriminator"}: {{ID: otherID, Path: "/mapping/d", Ref: "#/disc", Discriminator: true}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/1/discriminator"}: {
			{ID: discID, Path: "/mapping/d", Ref: "#/disc", Discriminator: true},
			{ID: defID, Path: "/defaultMapping", Ref: "#/def", Discriminator: true},
			{ID: discID, Path: "/unexpected", Ref: "#/disc", Discriminator: true},
		},
	}

	assert.True(t, openapi.AttachUnmodeledTargets(t.Context(), doc, landings))

	assert.Empty(t, root.Unmodeled["openapi:empty"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:num"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:other-src"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:plain-obj"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:unrelated-str"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:longer-base"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:bad-str"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:bad-obj"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:miss-str"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:disc-bad-mapping"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:disc-wrong-ref"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:root-collapsed"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"": subID}, root.Unmodeled["openapi:$ref"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"": subID}, root.Unmodeled["openapi:$dynamicRef"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"": subID}, root.Unmodeled["openapi:exact-str"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": subID}, root.Unmodeled["openapi:not"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/not/$ref": subID}, root.Unmodeled["openapi:residue"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/0/$ref": subID}, root.Unmodeled["openapi:arr"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": subID}, root.Unmodeled["openapi:schema/not"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/if/$ref": subID}, root.Unmodeled["openapi:if-then-else"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/contains/$ref": subID}, root.Unmodeled["openapi:contains"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{
		"/mapping/d":      discID,
		"/defaultMapping": defID,
	}, root.Unmodeled["openapi:discriminator"].Targets)

	// Stopped walks (truncated or cancelled) report false so pruning is skipped.
	truncatedStub := func(context.Context, any, string, ...ir.Visitor) (bool, error) { return true, nil }
	assert.False(t, openapi.AttachUnmodeledTargetsWith(t.Context(), doc, landings, truncatedStub))
	cancelledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.False(t, openapi.AttachUnmodeledTargets(cancelledCtx, doc, landings))
}

// TestSeedDiscardedLandings_SeedsDroppedProvenances pins seedDiscardedLandings
// across empty drops, unpreservable diagnostics, exact and descendant dropped
// sites (including document-root ""), and untouched live sites.
func TestSeedDiscardedLandings_SeedsDroppedProvenances(t *testing.T) {
	t.Parallel()
	const (
		droppedExact ir.TypeID = "type/anonymous/dropped-exact"
		droppedChild ir.TypeID = "type/anonymous/dropped-child"
		droppedRoot  ir.TypeID = "type/anonymous/dropped-root"
		liveOther    ir.TypeID = "type/anonymous/live-other"
		liveSrc      ir.TypeID = "type/anonymous/live-src"
	)
	landings := map[schema.LandingSite][]schema.Landing{
		{Source: 0, Pointer: "/components/schemas/A/not"}:        {{ID: droppedExact, Path: "/$ref", Ref: "#/a"}},
		{Source: 0, Pointer: "/components/headers/H/schema/not"}: {{ID: droppedChild, Path: "/$ref", Ref: "#/b"}},
		{Source: 0, Pointer: "/components/schemas/Live/not"}:     {{ID: liveOther, Path: "/$ref", Ref: "#/c"}},
		{Source: 1, Pointer: "/components/schemas/A/not"}:        {{ID: liveSrc, Path: "/$ref", Ref: "#/d"}},
		{Source: 2, Pointer: "/components/schemas/B/not"}:        {{ID: droppedRoot, Path: "/$ref", Ref: "#/e"}},
	}

	// Fast path: no unpreservable diagnostics and no dropped sites seeds nothing.
	discarded := map[ir.TypeID]bool{}
	openapi.SeedDiscardedLandings(discarded, landings, nil, []ir.Diagnostic{{Code: diag.DegradedConstruct}})
	assert.Empty(t, discarded)

	// Unpreservable diagnostic + dropped provenances seed exact and descendant sites only.
	openapi.SeedDiscardedLandings(
		discarded,
		landings,
		[]ir.Provenance{
			{Source: 0, Pointer: "/components/headers/H/schema"},
			{Source: 2, Pointer: ""},
		},
		[]ir.Diagnostic{
			{Code: diag.DegradedConstruct, Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Live/not"}},
			{Code: diag.UnpreservableConstruct, Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/A/not"}},
		},
	)
	assert.Equal(t, map[ir.TypeID]bool{
		droppedExact: true,
		droppedChild: true,
		droppedRoot:  true,
	}, discarded)
}

// TestRewritePrunedFoldDiagnostics_RewritesOnlyActuallyPrunedLosers pins that
// rewritePrunedFoldDiagnostics substitutes the alternate message only when the
// losing TypeID is absent from Document.Types and leaves surviving losers and
// unrelated diagnostics untouched.
func TestRewritePrunedFoldDiagnostics_RewritesOnlyActuallyPrunedLosers(t *testing.T) {
	t.Parallel()
	const (
		liveLoser   ir.TypeID = "type/anonymous/live-loser"
		prunedLoser ir.TypeID = "type/anonymous/pruned-loser"
	)
	types := ir.TypeRegistry{liveLoser: anonModel(liveLoser)}
	provLive := ir.Provenance{Source: 0, Pointer: "/components/schemas/A/allOf/0/properties/p"}
	provPruned := ir.Provenance{Source: 0, Pointer: "/components/schemas/B/allOf/0/properties/p"}

	diags := []ir.Diagnostic{
		{Code: diag.ConflictingRedecl, Provenance: provLive, Message: "full live"},
		{Code: diag.ConflictingRedecl, Provenance: provPruned, Message: "full pruned"},
		{Code: diag.DegradedConstruct, Provenance: provPruned, Message: "unrelated"},
	}

	// Empty candidates and surviving-only candidates leave all messages untouched.
	openapi.RewritePrunedFoldDiagnostics(diags, types, nil)
	openapi.RewritePrunedFoldDiagnostics(diags, types, []schema.PrunedDiagnostic{
		{Code: diag.ConflictingRedecl, Provenance: provLive, Full: "full live", Pruned: "pruned live", Loser: liveLoser},
	})
	assert.Equal(t, "full live", diags[0].Message)
	assert.Equal(t, "full pruned", diags[1].Message)

	// An actually pruned loser rewrites only its matching diagnostic.
	openapi.RewritePrunedFoldDiagnostics(diags, types, []schema.PrunedDiagnostic{
		{Code: diag.ConflictingRedecl, Provenance: provLive, Full: "full live", Pruned: "pruned live", Loser: liveLoser},
		{Code: diag.ConflictingRedecl, Provenance: provPruned, Full: "full pruned", Pruned: "pruned msg", Loser: prunedLoser},
	})
	assert.Equal(t, "full live", diags[0].Message)
	assert.Equal(t, "pruned msg", diags[1].Message)
	assert.Equal(t, "unrelated", diags[2].Message)
}
