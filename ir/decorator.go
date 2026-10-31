// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// Decorated returns the reference td decorates, and false when td is no
// decorator (docs/ir/annotations.md#decorators): an anonymous Model or Scalar
// whose Base is set and which says nothing about a value that its Base does
// not. Beside its identity it may set only the annotations a use site can hold,
// Availability and Unmodeled, so any other field it sets, one the IR gains
// included, keeps it a type of its own.
func Decorated(td TypeDef) (TypeRef, bool) {
	var base *TypeRef
	var common *TypeCommon
	switch n := td.(type) {
	case *Model:
		if n == nil || !modelAddsNothing(n) {
			return TypeRef{}, false
		}
		base, common = n.Base, &n.TypeCommon
	case *Scalar:
		if n == nil || n.Constraints != nil || n.Encoding != nil {
			return TypeRef{}, false
		}
		base, common = n.Base, &n.TypeCommon
	default:
		return TypeRef{}, false
	}
	if base == nil || !common.Anonymous || !annotatesOnly(common) {
		return TypeRef{}, false
	}
	return *base, true
}

// modelAddsNothing reports whether every field of m but its TypeCommon and
// Base is zero: no member, requirement, conformance, mixin, catch-all,
// closure, bound or tag of its own.
func modelAddsNothing(m *Model) bool {
	return len(m.Properties) == 0 && len(m.Requires) == 0 &&
		len(m.Implements) == 0 && len(m.Mixins) == 0 &&
		m.AdditionalProps == nil && m.Additional == AdditionalUnspecified && m.Constraints == nil &&
		!m.Abstract && !m.Positional && len(m.ExtensionRanges) == 0 &&
		m.Discriminator == nil && m.DiscriminatorValue == "" && !m.InputOnly
}

// annotatesOnly reports whether c sets nothing beyond a decorator's identity
// (ID, Name, Namespace, Anonymous, Provenance), the annotations a use site can
// hold (Docs, Deprecation, Examples), Availability, which no emitter
// interprets, and Unmodeled, whose entries an SDK acts on by reason and never
// by changing a type.
func annotatesOnly(c *TypeCommon) bool {
	return len(c.Tags) == 0 && !c.Sensitive && c.Access == "" &&
		len(c.WireNameByFormat) == 0 && c.MediaTypeHint == "" &&
		c.XML == nil && c.Instantiation == nil
}
