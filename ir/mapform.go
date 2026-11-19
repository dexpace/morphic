// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// AsMap returns the MapT that model is when model is a map and nothing else,
// and false otherwise (docs/ir/containers.md). It is the one reading of that
// rule: a compiler converts the Model it built through it, and irverify holds a
// document to it.
//
// The MapT keeps model's TypeCommon, so its ID, name and annotations, and takes
// the catch-all's key and value and model's Constraints. It shares their
// slices, maps and pointers with model, so a caller converting model replaces
// it rather than keeping both. A nil model is no map.
func AsMap(model *Model) (*MapT, bool) {
	if model == nil || !mapOnly(model) {
		return nil, false
	}
	return &MapT{
		TypeCommon:  model.TypeCommon,
		Key:         model.AdditionalProperties.Key,
		Value:       *model.AdditionalProperties.Value,
		Constraints: model.Constraints,
	}, true
}

// mapOnly reports whether model's one member fact is its catch-all: one value
// type under every key, with no pattern-scoped value, property, required
// member, composition, closure, discriminator or other model-only fact beside
// it. Each field of Model is either carried by AsMap or required empty here.
func mapOnly(model *Model) bool {
	ap := model.AdditionalProperties
	if ap == nil || ap.Value == nil || len(ap.Patterns) > 0 {
		return false
	}
	return len(model.Properties) == 0 && len(model.Requires) == 0 &&
		model.Base == nil && len(model.Implements) == 0 && len(model.Mixins) == 0 &&
		model.Additional == AdditionalUnspecified &&
		!model.Abstract && !model.Positional && len(model.ExtensionRanges) == 0 &&
		model.Discriminator == nil && model.DiscriminatorValue == "" &&
		!model.InputOnly
}
