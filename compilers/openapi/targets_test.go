// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// TestAttachUnmodeledTargets_MatchesEveryEntryForm pins attachUnmodeledTargets
// across empty landings, dead landings, string entries, object/array trees,
// discriminator routes, collapsed allOf provenance, and non-matching payloads.
func TestAttachUnmodeledTargets_MatchesEveryEntryForm(t *testing.T) {
	t.Parallel()
	const (
		subID  ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Sub"
		discID ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Disc"
		deadID ir.TypeID = "type/anonymous/not-in-registry"
	)

	// Fast paths: nil landings and landings naming no live type do nothing.
	doc := &ir.Document{Types: ir.TypeRegistry{rootModelID: rootModel()}}
	openapi.AttachUnmodeledTargets(t.Context(), doc, nil)
	openapi.AttachUnmodeledTargets(t.Context(), doc, map[schema.LandingSite][]schema.Landing{
		{Source: 0, Pointer: "/components/schemas/Root/not"}: {{ID: deadID, Ref: "#/dead"}},
	})

	root := rootModel()
	root.Unmodeled = ir.Unmodeled{
		"openapi:empty":       {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`   `), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/x-empty"}},
		"openapi:num":         {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`42`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/x-num"}},
		"openapi:other-src":   {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/sub"}`), Provenance: ir.Provenance{Source: 1, Pointer: "/components/schemas/Root/not"}},
		"openapi:plain-obj":   {Reason: ir.ReasonVendorExtension, Value: ir.RawValue(`{"k":"v"}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/x-obj"}},
		"openapi:bad-str":     {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"unterminated`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/$ref"}},
		"openapi:bad-obj":     {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/not"}},
		"openapi:$ref":        {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"#/sub"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/0/$ref"}},
		"openapi:$dynamicRef": {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"#/sub"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"}},
		"openapi:not":         {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/sub"}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/not"}},
		"openapi:arr":         {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`[{"$ref":"#/sub"}]`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/oneOf"}},
		"openapi:schema/not":  {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/sub"}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"}},
		"openapi:discriminator": {
			Reason:     ir.ReasonDegradedLowering,
			Value:      ir.RawValue(`{"propertyName":"k","mapping":{"d":"#/disc"}}`),
			Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"},
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
		},
	}

	landings := map[schema.LandingSite][]schema.Landing{
		{Source: 0, Pointer: "/components/schemas/Root/allOf/0"}:               {{ID: subID, Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/not"}:                   {{ID: subID, Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/oneOf/0"}:               {{ID: subID, Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/1/not"}:           {{ID: subID, Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/1/discriminator"}: {{ID: discID}},
	}

	openapi.AttachUnmodeledTargets(t.Context(), doc, landings)

	assert.Empty(t, root.Unmodeled["openapi:empty"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:num"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:other-src"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:plain-obj"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:bad-str"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:bad-obj"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:root-collapsed"].Targets)
	assert.Equal(t, []ir.TypeID{subID}, root.Unmodeled["openapi:$ref"].Targets)
	assert.Equal(t, []ir.TypeID{subID}, root.Unmodeled["openapi:$dynamicRef"].Targets)
	assert.Equal(t, []ir.TypeID{subID}, root.Unmodeled["openapi:not"].Targets)
	assert.Equal(t, []ir.TypeID{subID}, root.Unmodeled["openapi:arr"].Targets)
	assert.Equal(t, []ir.TypeID{subID}, root.Unmodeled["openapi:schema/not"].Targets)
	assert.Equal(t, []ir.TypeID{discID}, root.Unmodeled["openapi:discriminator"].Targets)
}
