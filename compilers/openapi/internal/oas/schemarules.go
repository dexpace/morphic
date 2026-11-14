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
	// object is the Schema Object as these rows read it, which the check
	// holds a schema read with them to: OpenAPI's own fields at minor, then
	// the dialect's keywords, each in table order, and the object's rules
	// with the dialect's own.
	object Object
	fields map[string]Field
	// later is each field the dialect the compiler reads natively defines
	// and these rows do not, by name, as that dialect's row gives it: at 3.0
	// every Schema Object field 3.1 added, and in a draft the minor's own
	// keywords the draft lacks, bar an identifier it spells otherwise.
	later    map[string]Field
	required []Rule
	minor    Minor
	// ignoresRefFields reports that the dialect, not the minor, ignores the
	// keywords beside a `$ref`.
	ignoresRefFields bool
	// booleans reports that true and false stand as schemas: not at 3.0, nor
	// in draft 4.
	booleans bool
	// draft reports rows of a JSON Schema draft rather than the minor's own,
	// which words a finding about a schema read with them.
	draft bool
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
	d, unread := dialectOf(minor, dialect)
	rows := &schemaRows{minor: minor, ignoresRefFields: d.ignoresRefFields, draft: d.keywords != nil,
		booleans: obj.Boolean.Holds(minor)}
	rows.object = obj
	rows.object.Fields = definedAt(obj, minor, d)
	if rows.draft {
		rows.booleans = d.booleans
		if d.rules != nil {
			rows.object.Rules = slices.Concat(obj.Rules, d.rules())
		}
	}
	rows.object.Boolean = Span{}
	if rows.booleans {
		rows.object.Boolean = Always()
	}
	rows.fields = make(map[string]Field, len(rows.object.Fields))
	for _, f := range rows.object.Fields {
		rows.fields[f.Name] = f
	}
	rows.later = laterThan(obj, minor, rows.draft, rows.fields)
	for _, r := range obj.Rules {
		if r.Kind == RuleRequiredWhen && r.Span.Holds(minor) {
			rows.required = append(rows.required, r)
		}
	}
	return SchemaRules{rows: rows}, unread
}

// definedAt returns the Schema Object's fields obj defines at minor in dialect
// d, in table order: every one the minor defines where d reads the minor's own
// keywords, else OpenAPI's own fields and then d's keywords.
func definedAt(obj Object, minor Minor, d dialect) []Field {
	var keywords []Field
	if d.keywords != nil {
		keywords = readBounds(d.keywords())
	}
	defined := make([]Field, 0, len(obj.Fields)+len(keywords))
	for _, f := range obj.Fields {
		if f.Defined.Holds(minor) && (keywords == nil || !f.Keyword) {
			defined = append(defined, f)
		}
	}
	return append(defined, keywords...)
}

// laterThan returns the fields of obj the dialect the compiler reads natively
// defines and fields, rows read at minor, do not, by name: 3.1's for a 3.0
// schema, and for a draft's, the minor's own keywords, bar `$id` where the
// draft's identifier is `id`, which reads it for nothing (GitHub #1467). The
// minor's own rows have none.
func laterThan(obj Object, minor Minor, draft bool, fields map[string]Field) map[string]Field {
	native := minor
	switch {
	case minor == V30:
		native = V31
	case !draft:
		return nil
	}
	_, spelledOtherwise := fields["id"]
	later := map[string]Field{}
	for _, f := range obj.Fields {
		if _, defined := fields[f.Name]; defined || !f.Defined.Holds(native) || draft && !f.Keyword {
			continue
		}
		if f.Name != "$id" || !spelledOtherwise {
			later[f.Name] = f
		}
	}
	return later
}

// dialectOf returns the dialect a schema written at minor and naming id is
// read in, the zero dialect, the minor's own, where it names none the table
// has rows for, and whether id is an absolute URI the table has no rows for. A
// dialect is compared without a trailing empty fragment, which draft 4's id
// writes and 2020-12's does not, as `$schema` values spell both.
func dialectOf(minor Minor, id string) (d dialect, unread bool) {
	if id == "" || minor == V30 {
		return dialect{}, false
	}
	trimmed := strings.TrimSuffix(id, "#")
	for _, known := range schemaDialects() {
		if strings.TrimSuffix(known.id, "#") == trimmed {
			return known, false
		}
	}
	return dialect{}, NamesDialect(id)
}

// IgnoresRefFields reports whether the dialect the rows are read in, rather
// than their minor, ignores the keywords a schema writes beside its `$ref`, as
// the JSON Schema drafts before 2019-09 do.
func (r SchemaRules) IgnoresRefFields() bool { return r.rows != nil && r.rows.ignoresRefFields }

// Later reports whether name is a keyword the rows do not define and the
// dialect the compiler reads natively does, which it reads as that dialect
// does: a keyword 2020-12 added, in a 3.0 schema or in a draft's.
func (r SchemaRules) Later(name string) bool {
	if r.rows == nil {
		return false
	}
	_, ok := r.rows.later[name]
	return ok
}

// Booleans reports whether true and false stand as schemas in the rows'
// dialect, as they do from 3.1 bar in JSON Schema draft 4.
func (r SchemaRules) Booleans() bool { return r.rows != nil && r.rows.booleans }

// Identifier returns the keyword the rows name a schema's identifier by: `$id`,
// or `id` where they define it alone, as JSON Schema draft 4 does, and "" where
// they define neither, as 3.0's do.
func (r SchemaRules) Identifier() string {
	switch {
	case r.Defines("$id"):
		return "$id"
	case r.Defines("id"):
		return "id"
	default:
		return ""
	}
}

// Formatted reports whether the rows give the field name the format f.
func (r SchemaRules) Formatted(name string, f Format) bool {
	fld, ok := r.field(name)
	return ok && fld.Format == f
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
