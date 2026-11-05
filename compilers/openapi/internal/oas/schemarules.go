// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
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
	fields map[string]Field
	// own is the minor's own keywords a dialect reading others replaces, by
	// name, which judge the format of one the dialect defines none of
	// (formatField).
	own      map[string]Field
	required []Rule
	minor    Minor
	// ignoresRefFields reports that the dialect, not the minor, ignores the
	// keywords beside a `$ref`.
	ignoresRefFields bool
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
	var keywords []Field
	if d.keywords != nil {
		keywords = d.keywords()
	}
	rows := &schemaRows{fields: make(map[string]Field, len(obj.Fields)), minor: minor, ignoresRefFields: d.ignoresRefFields}
	for _, f := range obj.Fields {
		switch {
		case !f.Defined.Holds(minor):
		case keywords == nil || !f.Keyword:
			rows.fields[f.Name] = f
		default:
			if rows.own == nil {
				rows.own = map[string]Field{}
			}
			rows.own[f.Name] = f
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

// FormatFinding returns the finding about o, a string value of a field whose
// format the rows' dialect decides (Field.FormatOwner), where it is not of the
// format the rows give the field, and false where it is, or the rows give the
// field none.
func (r SchemaRules) FormatFinding(o Owned) (ir.Diagnostic, bool) {
	f, ok := r.formatField(o.Field)
	if !ok || f.Format == FormatNone || validFormat(f.Format, o.Value.Value) {
		return ir.Diagnostic{}, false
	}
	return o.Finding(diag.Validation+"/"+classFormat, "Schema Object field %q must be %s in this schema's dialect",
		o.Field, describeFormat(f.Format)), true
}

// formatField returns the row giving the field name its format: the dialect's,
// or the minor's own for a keyword the dialect defines none of, as a keyword
// 2020-12 added reads in a draft's schema (GitHub #820), an `$anchor` in draft
// 7 among them. An `$id` the dialect spells otherwise, as draft 4 does `id`, is
// read for nothing, so nothing gives it one.
func (r SchemaRules) formatField(name string) (Field, bool) {
	if f, ok := r.field(name); ok || r.rows == nil || name == "$id" {
		return f, ok
	}
	f, ok := r.rows.own[name]
	return f, ok
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
