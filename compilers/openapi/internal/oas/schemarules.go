// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import "slices"

// SchemaRules is the Schema Object's rows at one minor: how a schema written
// at that minor reads, asked of the table rather than of the version, so each
// such fact is stated once, in a row citing its sentence. It has no accessor
// for its minor, so a holder cannot branch on one. The zero SchemaRules, a
// version the table does not cover, answers false to every question.
type SchemaRules struct{ rows *schemaRows }

// schemaRows is the Schema Object's fields defined at minor, by name, and the
// RuleRequiredWhen rules holding there. It is built once and never written.
type schemaRows struct {
	fields   map[string]Field
	required []Rule
	minor    Minor
}

// SchemaRules returns the Schema Object's rows at the minor version names, and
// the zero SchemaRules for a version the table does not cover or a Spec
// holding no Schema Object. The minor alone chooses them: a schema's `$schema`
// and a document's jsonSchemaDialect choose nothing yet (GitHub #1449).
func (s Spec) SchemaRules(version string) SchemaRules {
	minor, ok := ParseMinor(version)
	if !ok {
		return SchemaRules{}
	}
	obj, ok := s.Object(Schema)
	if !ok {
		return SchemaRules{}
	}
	rows := &schemaRows{fields: make(map[string]Field, len(obj.Fields)), minor: minor}
	for _, f := range obj.Fields {
		if f.Defined.Holds(minor) {
			rows.fields[f.Name] = f
		}
	}
	for _, r := range obj.Rules {
		if r.Kind == RuleRequiredWhen && r.Span.Holds(minor) {
			rows.required = append(rows.required, r)
		}
	}
	return SchemaRules{rows: rows}
}

// Defines reports whether the Schema Object defines the field name at the
// minor: a keyword of its dialect, or a field OpenAPI adds to it.
func (r SchemaRules) Defines(name string) bool {
	_, ok := r.field(name)
	return ok
}

// Takes reports whether the field name, defined at the minor, takes a value of
// shape there: its own shape, or another it admits from a minor up to this one.
func (r SchemaRules) Takes(name string, shape Shape) bool {
	f, ok := r.field(name)
	if !ok {
		return false
	}
	return f.Shape == shape || slices.ContainsFunc(f.Alts, func(a Alt) bool {
		return a.Shape == shape && a.Span.Holds(r.rows.minor)
	})
}

// RequiredWhen reports whether the minor requires the field name in a schema
// whose field beside holds value: a RuleRequiredWhen on name holds there whose
// every term reads beside and admits value, so value alone decides it.
func (r SchemaRules) RequiredWhen(name, beside, value string) bool {
	if r.rows == nil {
		return false
	}
	return slices.ContainsFunc(r.rows.required, func(rule Rule) bool {
		return rule.Field == name && !slices.ContainsFunc(rule.When, func(t Term) bool {
			return t.Field != beside || !slices.Contains(t.In, value)
		})
	})
}

// field returns the field name the Schema Object defines at the minor.
func (r SchemaRules) field(name string) (Field, bool) {
	if r.rows == nil {
		return Field{}, false
	}
	f, ok := r.rows.fields[name]
	return f, ok
}
