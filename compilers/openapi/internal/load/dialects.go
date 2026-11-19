// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// dialects reads the minor a document is read at: the source's, and for one a
// reference reaches the one its declared version names, or the source's where
// the table covers none, as for a bare schema file, which declares no version,
// or a Swagger 2.0 one. It reads each dialect's rows (Rows) at each minor from
// spec once, so the documents and the schemas naming one share them; a map
// keyed by both holds no more entries than the table has minors times the
// dialects named.
type dialects struct {
	spec specrules.Spec
	// version and minor are the source's, which the load phase has admitted,
	// and dialect its jsonSchemaDialect.
	version, minor, dialect string
	rows                    map[dialectKey]dialectRows
}

// dialectKey is a minor and the dialect its rows are read in, as written.
type dialectKey struct {
	minor, dialect string
}

// dialectRows is a dialect's rows at a minor, and whether it is one the table
// has no Schema Object rows for (specrules.Spec.SchemaRules).
type dialectRows struct {
	rows   Rows
	unread bool
}

// of returns the version a document declaring declared is held to, its minor,
// and what the table says at that minor in the dialect uri, its
// jsonSchemaDialect, names, which its schemas are read with where none names a
// dialect of its own.
func (d *dialects) of(declared, uri string) (version, minor string, rows Rows) {
	version, minor = d.version, d.minor
	if m, ok := supportedMinor(declared); ok {
		version, minor = declared, m
	}
	return version, minor, d.at(version, uri).rows
}

// own returns what the table says at the source's minor, in the dialect its
// jsonSchemaDialect names.
func (d *dialects) own() Rows {
	return d.at(d.version, d.dialect).rows
}

// at returns the rows of the dialect uri names at the minor version names.
func (d *dialects) at(version, uri string) dialectRows {
	minor, _ := supportedMinor(version)
	dialectKeyValue := dialectKey{minor: minor, dialect: uri}
	if dialectRows, built := d.rows[dialectKeyValue]; built {
		return dialectRows
	}
	if d.rows == nil {
		d.rows = map[dialectKey]dialectRows{}
	}
	rows, unread := rowsAt(d.spec, version, uri)
	d.rows[dialectKeyValue] = dialectRows{rows: rows, unread: unread}
	return d.rows[dialectKeyValue]
}

// readDialects returns the rows of each dialect a schema of the documents table
// read names, and an openapi/unread-dialect warning for each absolute URI
// naming one the table has no rows for, whose schemas read their minor's own:
// at each such `$schema`, placed by sites, and at each document's
// jsonSchemaDialect, the source's among them. A value no absolute URI names
// chooses nothing and draws none: validation reports it where it judges it,
// which in a document a reference reaches is only what the reference reaches.
// Each document is read under one name (componentlookup.Locate), so warned of
// once.
func readDialects(table *reftable.ReferenceTargetTable, others Others, dialectsValue *dialects, sites places) (dialect.SchemaDialectRows, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	unread := func(at ir.Provenance, keyword, uri string) {
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.UnreadDialect, at,
			"%s names %q, a JSON Schema dialect the compiler has no rows for, so the schemas it governs are "+
				"read in the dialect their OpenAPI version names", keyword, uri))
	}
	at := jsontext.Pointer("").AppendToken("jsonSchemaDialect")
	if dialectsValue.at(dialectsValue.version, dialectsValue.dialect).unread {
		unread(sites.position(reftable.DocumentPointer{Doc: reftable.Source, Pointer: at}), "jsonSchemaDialect", dialectsValue.dialect)
	}
	for _, other := range others.List() {
		if dialectsValue.at(other.version, other.dialect).unread {
			unread(provenance.Doc{Source: other.Index}.At(at), "jsonSchemaDialect", other.dialect)
		}
	}
	source := dialectsValue.at(dialectsValue.version, dialectsValue.dialect).rows.Schema
	b := dialect.NewBuilder(source)
	for id := range reftable.DocID(table.Documents()) {
		version, ok := versionOf(table, others, dialectsValue, id)
		if !ok {
			continue
		}
		own := source
		if other, listed := others.Named(table.Document(id).Name); listed && id != reftable.Source {
			own = dialectsValue.at(other.version, other.dialect).rows.Schema
			b.Document(id, own)
		}
		// Innermost first: a root's pointer extends that of a root holding it,
		// so it sorts after, and the list is in pointer order.
		for _, root := range slices.Backward(table.Document(id).Dialects) {
			dialectRows := dialectsValue.at(version, root.URI)
			if dialectRows.unread {
				unread(sites.position(reftable.DocumentPointer{Doc: id, Pointer: root.At.Pointer.AppendToken("$schema")}), "$schema",
					root.URI)
			}
			b.Root(root.At, root.Node, dialectRows.rows.Schema)
		}
		// Outermost last: a node no root holds reads its document's own rows,
		// noted only where they are not the source's, which Table.Of gives a
		// node it notes nothing of.
		if own != source {
			b.Govern(table.Document(id).Root, own)
		}
	}
	return b.Table(), diags
}

// versionOf returns the version the schemas of document id of table are read
// at, and false for a document the compile did not read beside the source.
func versionOf(table *reftable.ReferenceTargetTable, others Others, dialectsValue *dialects, id reftable.DocID) (string, bool) {
	if id == reftable.Source {
		return dialectsValue.version, true
	}
	other, ok := others.Named(table.Document(id).Name)
	return other.version, ok
}

// dialectRuleOf returns what the binder reads otherwise in uri's dialect, whose
// rows rules are, than in 2020-12 (reftable.DialectRule): the identifier,
// `$id`, or draft 4's `id` where the rows define it alone; one naming a
// fragment alone as an anchor where they define no `$anchor`, by the plain-name
// grammar where they give the identifier it; that grammar for `$anchor` where
// they give it; `definitions`, `dependencies`'s schemas and `$recursiveAnchor`
// where they define them; an `items` list as a tuple where they take one; no
// identifier beside a `$ref` where they ignore what is there; and whether uri
// names one at all (specrules.NamesDialect).
func dialectRuleOf(rules specrules.SchemaRules, uri string) reftable.DialectRule {
	rule := reftable.DialectRule{Keyword: rules.Identifier(), Definitions: rules.Takes("definitions", specrules.Map),
		Dependencies: rules.Defines("dependencies"), Tuples: rules.Takes("items", specrules.List),
		Recursive: rules.Defines("$recursiveRef"), IgnoresRefFields: rules.IgnoresRefFields(),
		NamesNone: !specrules.NamesDialect(uri)}
	rule.Fragments = rule.Keyword != "" && !rules.Defines("$anchor")
	rule.PlainFragments = rules.Formatted(rule.Keyword, specrules.PlainNameURI)
	rule.PlainNames = rules.Formatted("$anchor", specrules.PlainName)
	return rule
}
