// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"slices"
	"strings"
)

// SchemaRules is the Schema Object's rows at one minor in one dialect: how a
// schema written so reads, asked of the table rather than of the version, so
// each such fact is stated once, in a row citing its sentence. It has no
// accessor for its minor or dialect, so a holder cannot branch on either. The
// zero SchemaRules, a version the table does not cover, answers false to
// every question.
type SchemaRules struct{ rows *schemaRows }

// schemaRows is the Schema Object's fields defined at minor in a dialect, by
// name, and the RuleRequiredWhen rules holding there. It is built once and
// never written.
type schemaRows struct {
	fields   map[string]Field
	required []Rule
	minor    Minor
}

// SchemaRules returns the Schema Object's rows at the minor version names, in
// the dialect a `$schema` or a jsonSchemaDialect names by its URI, and whether
// that is an absolute URI naming a dialect the table has no rows for, which
// reads as the minor's own. An empty dialect, any at 3.0, which defines neither
// keyword, and one no absolute URI names, which validation reports where it
// judges the value, are the minor's own too. A version the table does not
// cover, or a Spec holding no Schema Object, has the zero rows.
func (s Spec) SchemaRules(version, dialect string) (rules SchemaRules, unread bool) {
	minor, ok := ParseMinor(version)
	if !ok {
		return SchemaRules{}, false
	}
	obj, ok := s.Object(Schema)
	if !ok {
		return SchemaRules{}, false
	}
	keywords, unread := dialectKeywords(minor, dialect)
	rows := &schemaRows{fields: make(map[string]Field, len(obj.Fields)), minor: minor}
	for _, f := range obj.Fields {
		if f.Defined.Holds(minor) && (keywords == nil || !f.Keyword) {
			rows.fields[f.Name] = f
		}
	}
	for _, f := range keywords {
		rows.fields[f.Name] = f
	}
	for _, r := range obj.Rules {
		if r.Kind == RuleRequiredWhen && r.Span.Holds(minor) {
			rows.required = append(rows.required, r)
		}
	}
	return SchemaRules{rows: rows}, unread
}

// dialectKeywords returns the keywords dialect reads in place of minor's own,
// nil where it reads minor's, and whether it is an absolute URI the table has
// no rows for. A dialect is compared without a trailing empty fragment, which
// draft 4's id writes and 2020-12's does not, as `$schema` values spell both.
func dialectKeywords(minor Minor, dialect string) (keywords []Field, unread bool) {
	if dialect == "" || minor == V30 {
		return nil, false
	}
	id := strings.TrimSuffix(dialect, "#")
	for _, d := range schemaDialects() {
		if strings.TrimSuffix(d.id, "#") != id {
			continue
		}
		if d.keywords == nil {
			return nil, false
		}
		return d.keywords(), false
	}
	return nil, NamesDialect(dialect)
}

// NamesDialect reports whether a `$schema` or jsonSchemaDialect value names a
// dialect at all: an absolute URI, as each must be. One that names none,
// which validation reports, chooses nothing, as though it were not written.
func NamesDialect(dialect string) bool { return validFormat(AbsoluteURI, dialect) }

// Defines reports whether the Schema Object defines the field name: a keyword
// of its dialect, or a field OpenAPI adds to it at the minor.
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

// field returns the field name the Schema Object defines in the rows.
func (r SchemaRules) field(name string) (Field, bool) {
	if r.rows == nil {
		return Field{}, false
	}
	f, ok := r.rows.fields[name]
	return f, ok
}
