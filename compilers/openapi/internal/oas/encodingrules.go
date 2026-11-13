// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// EncodingRules is the Encoding Object's rows at one minor, as SchemaRules is
// the Schema Object's: what a part takes where its Encoding Object leaves a
// field unwritten, asked of the table rather than of the version. The zero
// EncodingRules, a version the table does not cover, states no default.
type EncodingRules struct{ rows *encodingRows }

// encodingRows is the defaults the Encoding Object's fields take at one minor,
// by field and case, the fields the table limits to some media types, and the
// media types each takes effect under there. It is built once and never
// written.
type encodingRows struct {
	defaults map[defaultOf]string
	limited  map[string]bool
	within   map[withinOf]bool
}

// defaultOf keys a field's default in one case.
type defaultOf struct {
	field string
	k     DefaultCase
}

// withinOf keys a field taking effect under one media type.
type withinOf struct{ field, mediaType string }

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
	rows := &encodingRows{defaults: map[defaultOf]string{}, limited: map[string]bool{}, within: map[withinOf]bool{}}
	for _, d := range obj.Defaults {
		if d.Span.Holds(minor) {
			rows.defaults[defaultOf{field: d.Field, k: d.Case}] = d.Value
		}
	}
	for _, f := range obj.Fields {
		for _, w := range f.Within {
			rows.limited[f.Name] = true
			if w.Span.Holds(minor) {
				rows.within[withinOf{field: f.Name, mediaType: w.MediaType}] = true
			}
		}
	}
	return EncodingRules{rows: rows}
}

// ContentType returns the contentType a value of case k takes where its
// Encoding Object writes none, and false where the minor gives that case no
// default.
func (r EncodingRules) ContentType(k DefaultCase) (string, bool) {
	if r.rows == nil {
		return "", false
	}
	v, ok := r.rows.defaults[defaultOf{field: "contentType", k: k}]
	return v, ok
}

// TakesEffect reports whether the Encoding Object's field takes effect under
// mediaType, which names no parameters and is in lower case: under every media
// type where the table limits it to none, else under those it names at the
// minor. The zero EncodingRules answers false.
func (r EncodingRules) TakesEffect(field, mediaType string) bool {
	if r.rows == nil {
		return false
	}
	return !r.rows.limited[field] || r.rows.within[withinOf{field: field, mediaType: mediaType}]
}
