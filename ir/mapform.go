// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// AsMap returns the MapT that m is when m is a map and nothing else, and false
// otherwise (docs/ir/containers.md). It is the one reading of that rule: a
// compiler converts the Model it built through it, and irverify holds a
// document to it.
//
// The MapT keeps m's TypeCommon, so its ID, name and annotations, and takes the
// catch-all's key and value and m's Constraints. It shares their slices, maps
// and pointers with m, so a caller converting m replaces it rather than keeping
// both. A nil m is no map.
func AsMap(m *Model) (*MapT, bool) {
	if m == nil || !mapOnly(m) {
		return nil, false
	}
	return &MapT{
		TypeCommon:  m.TypeCommon,
		Key:         m.AdditionalProps.Key,
		Value:       m.AdditionalProps.Value,
		Constraints: m.Constraints,
	}, true
}

// mapOnly reports whether m's one member fact is its catch-all: one value type
// under every key, with no pattern-scoped value, property, required member,
// composition, closure, discriminator or other model-only fact beside it. Each
// field of Model is either carried by AsMap or required empty here.
func mapOnly(m *Model) bool {
	ap := m.AdditionalProps
	if ap == nil || len(ap.Patterns) > 0 {
		return false
	}
	return len(m.Properties) == 0 && len(m.Requires) == 0 &&
		m.Base == nil && len(m.Implements) == 0 && len(m.Mixins) == 0 &&
		m.Additional == AdditionalUnspecified &&
		!m.Abstract && !m.Positional && len(m.ExtensionRanges) == 0 &&
		m.Discriminator == nil && m.DiscriminatorValue == "" &&
		!m.InputOnly
}
