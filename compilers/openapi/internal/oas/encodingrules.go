// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// EncodingRules is the Encoding Object's rows at one minor, as SchemaRules is
// the Schema Object's: what a part takes where its Encoding Object leaves a
// field unwritten, asked of the table rather than of the version. The zero
// EncodingRules, a version the table does not cover, states no default.
type EncodingRules struct{ rows *encodingRows }

// encodingRows is the defaults the Encoding Object's fields take at one minor,
// by field and case. It is built once and never written.
type encodingRows struct {
	defaults map[defaultOf]string
}

// defaultOf keys a field's default in one case.
type defaultOf struct {
	field string
	k     DefaultCase
}

// EncodingRules returns the Encoding Object's rows at the minor version names,
// and the zero EncodingRules for a version the table does not cover or a Spec
// holding no Encoding Object.
func (s Spec) EncodingRules(version string) EncodingRules {
	minor, ok := ParseMinor(version)
	if !ok {
		return EncodingRules{}
	}
	obj, ok := s.Object(Encoding)
	if !ok {
		return EncodingRules{}
	}
	rows := &encodingRows{defaults: map[defaultOf]string{}}
	for _, d := range obj.Defaults {
		if d.Span.Holds(minor) {
			rows.defaults[defaultOf{field: d.Field, k: d.Case}] = d.Value
		}
	}
	return EncodingRules{rows: rows}
}

// UntypedContentType returns the contentType a part whose schema writes no
// type takes where its Encoding Object writes none, and false where the minor
// gives that case no default.
func (r EncodingRules) UntypedContentType() (string, bool) {
	if r.rows == nil {
		return "", false
	}
	v, ok := r.rows.defaults[defaultOf{field: "contentType", k: DefaultUntyped}]
	return v, ok
}
