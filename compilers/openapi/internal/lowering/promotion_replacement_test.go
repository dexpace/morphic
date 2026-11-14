// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// TestPromoter_ReadsReplacementNames pins what a replacement key's value says:
// a string is one name and a list several, an empty string names nothing, and
// any other value, or a list holding an empty name, is a misfit, reported at
// the key. The provenance returned is the stating key's.
func TestPromoter_ReadsReplacementNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, value string
		want        []string
		misfit      bool
	}{
		{"one name", `"v2"`, []string{"v2"}, false},
		{"several names", `["v2","v3"]`, []string{"v2", "v3"}, false},
		{"an empty name", `""`, nil, false},
		{"null", `null`, nil, false},
		{"a number", `3`, nil, true},
		{"a list with an empty name", `["v2",""]`, nil, true},
		{"a list of numbers", `[1]`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := promoter(lowering.ExtensionPromotions{Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
				lowering.TargetDeprecationReplacedBy: {{Key: "x-gone"}, {Key: "x-replaced-by"}},
			}})
			entry := vendorExtension(tc.value)
			entry.Provenance.Pointer = "/paths/~1a/get/x-replaced-by"
			names, at, diags := p.Replacements(ir.Unmodeled{"openapi:x-replaced-by": entry})
			assert.Equal(t, tc.want, names)
			assert.Equal(t, tc.misfit, len(diags) == 1, "a misfit is reported; got %v", diags)
			if tc.want != nil {
				assert.Equal(t, jsontext.Pointer("/paths/~1a/get/x-replaced-by"), at.Pointer)
			}
		})
	}
}

// TestPromoter_ReplacementsUnmappedReadNothing pins the default: no key names
// the target, so a key a document writes is read only once a mapping names it.
func TestPromoter_ReplacementsUnmappedReadNothing(t *testing.T) {
	t.Parallel()
	names, _, diags := promoter(lowering.ExtensionPromotions{}).Replacements(ir.Unmodeled{"openapi:x-replaced-by": vendorExtension(`"v2"`)})
	assert.Empty(t, names)
	assert.Empty(t, diags)
}

// TestPromoter_MapsTellsAMappedTarget pins what lets a pass over the whole
// document skip the walk: a target the policy names sources for, and the
// default naming none for a replacement.
func TestPromoter_MapsTellsAMappedTarget(t *testing.T) {
	t.Parallel()
	mapped := promoter(mapping(lowering.TargetDeprecationReplacedBy, lowering.ExtensionSource{Key: "x-r"}))
	assert.True(t, mapped.Maps(lowering.TargetDeprecationReplacedBy))
	assert.False(t, promoter(lowering.ExtensionPromotions{}).Maps(lowering.TargetDeprecationReplacedBy),
		"the default mapping names no replacement key")
}

// TestPromoter_WritesSeesAKeyWhateverItSays pins what a reading of names asks
// before it fills a field: whether the document writes a key the policy reads
// the target from, in any position of the list and whatever its value,
// `false` and a misfit among them, and never a key mapped to another target.
func TestPromoter_WritesSeesAKeyWhateverItSays(t *testing.T) {
	t.Parallel()
	p := promoter(lowering.ExtensionPromotions{Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{
		lowering.TargetRedaction:             {{Key: "x-a"}, {Key: "x-b"}},
		lowering.TargetDeprecationReplacedBy: {{Key: "x-c"}},
	}})
	for _, tc := range []struct {
		name  string
		own   ir.Unmodeled
		wants bool
	}{
		{"the first key, true", ir.Unmodeled{"openapi:x-a": vendorExtension(`true`)}, true},
		{"the second key, false", ir.Unmodeled{"openapi:x-b": vendorExtension(`false`)}, true},
		{"a misfit", ir.Unmodeled{"openapi:x-a": vendorExtension(`"yes"`)}, true},
		{"a key mapped to another target", ir.Unmodeled{"openapi:x-c": vendorExtension(`true`)}, false},
		{"an unmapped key", ir.Unmodeled{"openapi:x-d": vendorExtension(`true`)}, false},
		{"none", nil, false},
	} {
		assert.Equal(t, tc.wants, p.Writes(tc.own, lowering.TargetRedaction), tc.name)
	}
	assert.False(t, promoter(lowering.ExtensionPromotions{Disabled: true}).Writes(
		ir.Unmodeled{"openapi:x-sensitive": vendorExtension(`true`)}, lowering.TargetRedaction),
		"a disabled policy reads no key")
}

// TestPromoter_WritesKnowsEveryTarget pins Writes to the vocabulary: a key a
// mapping names for any target, whatever it says, is the document speaking to
// that target and to no other. An inference asking of a target Writes did not
// know would fill over what a promotion fills.
func TestPromoter_WritesKnowsEveryTarget(t *testing.T) {
	t.Parallel()
	own := ir.Unmodeled{"openapi:x-k": vendorExtension(`false`)}
	for _, target := range lowering.Targets() {
		t.Run(string(target), func(t *testing.T) {
			t.Parallel()
			p := promoter(mapping(target, lowering.ExtensionSource{Key: "x-k", Member: "m"}))
			for _, other := range lowering.Targets() {
				assert.Equal(t, other == target, p.Writes(own, other), "asked of %s", other)
			}
		})
	}
}

// TestPromoter_WritesSeesEveryDefaultKey pins the same of the default mapping:
// each key it reads a target from, the server name's and both pagination
// conventions' among them, is the document speaking to that target.
func TestPromoter_WritesSeesEveryDefaultKey(t *testing.T) {
	t.Parallel()
	p := promoter(lowering.ExtensionPromotions{})
	for target, sources := range lowering.DefaultExtensionPromotions() {
		for _, src := range sources {
			own := ir.Unmodeled{"openapi:" + src.Key: vendorExtension(`{}`)}
			assert.True(t, p.Writes(own, target), "%s read for %s", src.Key, target)
		}
	}
}

// TestPromoter_ReplacementSourcesCompareTheirNames pins that two sources the
// policy maps agree when they name the same replacements, however each spells
// them, and disagree, the later reported, when they name others.
func TestPromoter_ReplacementSourcesCompareTheirNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, first, second string
		disagree            bool
	}{
		{"a name and a list of it", `"v2"`, `["v2"]`, false},
		{"a list of a name and the name", `["v2"]`, `"v2"`, false},
		{"two names", `"v2"`, `"v3"`, true},
		{"a name and a longer list", `"v2"`, `["v2","v3"]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := promoter(mapping(lowering.TargetDeprecationReplacedBy, lowering.ExtensionSource{Key: "x-a"},
				lowering.ExtensionSource{Key: "x-b"}))
			names, _, diags := p.Replacements(ir.Unmodeled{
				"openapi:x-a": vendorExtension(tc.first), "openapi:x-b": vendorExtension(tc.second),
			})
			assert.Equal(t, []string{"v2"}, names, "the first source is read")
			assert.Equal(t, tc.disagree, len(diags) == 1, "the later source is reported only where it differs: %v", diags)
		})
	}
}
