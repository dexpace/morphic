// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
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
	spec oas.Spec
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
// has no Schema Object rows for (oas.Spec.SchemaRules).
type dialectRows struct {
	rows   Rows
	unread bool
}

// of returns the version a document declaring declared is held to, its minor,
// and what the table says at that minor in dialect, its jsonSchemaDialect,
// which its schemas are read with where none names a dialect of its own.
func (d *dialects) of(declared, dialect string) (version, minor string, rows Rows) {
	version, minor = d.version, d.minor
	if m, ok := supportedMinor(declared); ok {
		version, minor = declared, m
	}
	return version, minor, d.at(version, dialect).rows
}

// own returns what the table says at the source's minor, in the dialect its
// jsonSchemaDialect names.
func (d *dialects) own() Rows {
	return d.at(d.version, d.dialect).rows
}

// at returns dialect's rows at the minor version names.
func (d *dialects) at(version, dialect string) dialectRows {
	minor, _ := supportedMinor(version)
	k := dialectKey{minor: minor, dialect: dialect}
	if r, built := d.rows[k]; built {
		return r
	}
	if d.rows == nil {
		d.rows = map[dialectKey]dialectRows{}
	}
	rows, unread := rowsAt(d.spec, version, dialect)
	d.rows[k] = dialectRows{rows: rows, unread: unread}
	return d.rows[k]
}

// Dialects is the rows the schemas a resource root naming its dialect holds
// (bind.Document.Dialects) are read with, by each node of that root's tree, a
// root it holds governing its own. A schema no such root holds reads its
// document's rows.
type Dialects struct {
	byNode map[*yaml.Node]oas.SchemaRules
}

// Of returns the rows the schema whose node is n is read with where a root
// holding it, itself included, names its dialect, the innermost such root's,
// and false where none does. A node an alias places at several positions reads
// one root's at all of them. No reading today tells one valid node's two
// dialects apart (GitHub #1457).
func (d Dialects) Of(n *yaml.Node) (oas.SchemaRules, bool) {
	r, ok := d.byNode[nodeview.Deref(n)]
	return r, ok
}

// govern notes that every node of the tree under root, which aliases place
// there included, reads rules, but those a root governed before it holds.
// Roots are governed innermost first, so a nested root keeps its own. Each
// node is read once, so the walk is bounded by the document's nodes.
func (d Dialects) govern(root *yaml.Node, rules oas.SchemaRules) {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := nodeview.Deref(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if _, governed := d.byNode[n]; governed {
			continue
		}
		d.byNode[n] = rules
		stack = append(stack, n.Content...)
	}
}

// readDialects returns the rows of each dialect a schema of the documents
// table read names, and an openapi/unread-dialect warning for each absolute URI
// naming one the table has no rows for, whose schemas read their minor's own:
// at each such `$schema`, placed by place, and at each document's
// jsonSchemaDialect, the source's among them. Validation
// reports a value no absolute URI names. A document two names read is warned
// of once.
func readDialects(table *bind.Table, others Others, d *dialects,
	place func(bind.Position) ir.Provenance,
) (Dialects, []ir.Diagnostic) {
	var diags []ir.Diagnostic
	type spot struct {
		source  int
		pointer jsontext.Pointer
	}
	warned := map[spot]bool{}
	unread := func(at ir.Provenance, keyword, dialect string) {
		if warned[spot{at.Source, at.Pointer}] {
			return
		}
		warned[spot{at.Source, at.Pointer}] = true
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.UnreadDialect, at,
			"%s names %q, a JSON Schema dialect the compiler has no rows for, so the schemas it governs are "+
				"read in the dialect their OpenAPI version names", keyword, dialect))
	}
	at := jsontext.Pointer("").AppendToken("jsonSchemaDialect")
	if d.at(d.version, d.dialect).unread {
		unread(place(bind.Position{Doc: bind.Source, Pointer: at}), "jsonSchemaDialect", d.dialect)
	}
	for _, o := range others.List() {
		if d.at(o.version, o.dialect).unread {
			unread(ir.Provenance{Source: o.Index, Pointer: at}, "jsonSchemaDialect", o.dialect)
		}
	}
	out := Dialects{byNode: map[*yaml.Node]oas.SchemaRules{}}
	for id := range bind.DocID(table.Documents()) {
		version, ok := versionOf(table, others, d, id)
		if !ok {
			continue
		}
		// Innermost first: a root's pointer extends that of a root holding it,
		// so it sorts after, and the list is in pointer order.
		for _, root := range slices.Backward(table.Document(id).Dialects) {
			r := d.at(version, root.URI)
			if r.unread {
				unread(place(bind.Position{Doc: id, Pointer: root.At.Pointer.AppendToken("$schema")}), "$schema", root.URI)
			}
			out.govern(root.Node, r.rows.Schema)
		}
	}
	return out, diags
}

// versionOf returns the version the schemas of document id of table are read
// at, and false for a document the compile did not read beside the source.
func versionOf(table *bind.Table, others Others, d *dialects, id bind.DocID) (string, bool) {
	if id == bind.Source {
		return d.version, true
	}
	o, ok := others.Named(table.Document(id).Name)
	return o.version, ok
}
