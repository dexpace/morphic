// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi"
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
		subID  ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Sub"
		discID ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Disc"
		defID  ir.TypeID = "type/anonymous/components/schemas/Root/$defs/Def"
		deadID ir.TypeID = "type/anonymous/not-in-registry"
	)

	// Fast paths: nil landings and landings naming no live type do nothing.
	doc := &ir.Document{Types: ir.TypeRegistry{rootModelID: rootModel()}}
	openapi.AttachUnmodeledTargets(t.Context(), doc, nil)
	openapi.AttachUnmodeledTargets(t.Context(), doc, map[schema.LandingSite][]schema.Landing{
		{Source: 0, Pointer: "/components/schemas/Root/not"}: {{ID: deadID, Path: "/$ref", Ref: "#/dead"}},
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
		"openapi:exact-str":   {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"#/sub"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/0"}},
		"openapi:miss-str":    {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"#/none"`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"}},
		"openapi:not":         {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/sub"}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/not"}},
		"openapi:residue":     {Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`{"not":{"$ref":"#/sub"}}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/allOf/0"}},
		"openapi:arr":         {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`[{"$ref":"#/sub"}]`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root/oneOf"}},
		"openapi:schema/not":  {Reason: ir.ReasonValidationOnly, Value: ir.RawValue(`{"$ref":"#/sub"}`), Provenance: ir.Provenance{Source: 0, Pointer: "/components/schemas/Root"}},
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
		},
	}

	landings := map[schema.LandingSite][]schema.Landing{
		{Source: 0, Pointer: "/components/schemas/Root/allOf/0"}:     {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/0/not"}: {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/not"}:         {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/oneOf/0"}:     {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/1/not"}: {{ID: subID, Path: "/$ref", Ref: "#/sub"}},
		{Source: 0, Pointer: "/components/schemas/Root/allOf/1/discriminator"}: {
			{ID: discID, Path: "/mapping/d"},
			{ID: defID, Path: "/defaultMapping"},
			{ID: discID, Path: "/unexpected"},
		},
	}

	openapi.AttachUnmodeledTargets(t.Context(), doc, landings)

	assert.Empty(t, root.Unmodeled["openapi:empty"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:num"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:other-src"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:plain-obj"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:bad-str"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:bad-obj"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:miss-str"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:disc-bad-mapping"].Targets)
	assert.Empty(t, root.Unmodeled["openapi:root-collapsed"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"": subID}, root.Unmodeled["openapi:$ref"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"": subID}, root.Unmodeled["openapi:$dynamicRef"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"": subID}, root.Unmodeled["openapi:exact-str"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": subID}, root.Unmodeled["openapi:not"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/not/$ref": subID}, root.Unmodeled["openapi:residue"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/0/$ref": subID}, root.Unmodeled["openapi:arr"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{"/$ref": subID}, root.Unmodeled["openapi:schema/not"].Targets)
	assert.Equal(t, map[jsontext.Pointer]ir.TypeID{
		"/mapping/d":      discID,
		"/defaultMapping": defID,
	}, root.Unmodeled["openapi:discriminator"].Targets)
}
