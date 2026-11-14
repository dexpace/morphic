// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// SecurityRules is the Security Requirement Object's rows at one minor, as
// EncodingRules is the Encoding Object's: how a requirement's key names a
// scheme, asked of the table rather than of the version. The zero
// SecurityRules, a version the table does not cover, reads a key as a name.
type SecurityRules struct{ byURI bool }

// SecurityRules returns the Security Requirement Object's rows at the minor
// version names, and the zero SecurityRules for a version the table does not
// cover or a Spec holding no Security Requirement Object.
func (s Spec) SecurityRules(version string) SecurityRules {
	minor, ok := ParseMinor(version)
	if !ok {
		return SecurityRules{}
	}
	obj, ok := s.Object(SecurityRequirement)
	if !ok {
		return SecurityRules{}
	}
	for _, p := range obj.Patterned {
		if p.ByURI && p.Span.Holds(minor) {
			return SecurityRules{byURI: true}
		}
	}
	return SecurityRules{}
}

// NamesByURI reports whether a requirement's key that is no component name
// may be the URI of a Security Scheme Object.
func (r SecurityRules) NamesByURI() bool { return r.byURI }
