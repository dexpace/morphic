// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"encoding/json/jsontext"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/ir"
)

// dialects reads the minor a document a reference reaches is read at: the one
// its declared version names, or the source's where the table covers none, as
// for a bare schema file, which declares no version, or a Swagger 2.0 one. It
// reads the Schema Object's rows of each dialect at each minor from spec once,
// so the documents and the schemas naming one share them; a map keyed by both
// holds no more entries than the table has minors times the dialects named.
type dialects struct {
	spec oas.Spec
	// version and minor are the source's, which the load phase has admitted.
	version, minor string
	rules          map[dialectKey]dialectRows
}

// dialectKey is a minor and the dialect its rows are read in, as written.
type dialectKey struct {
	minor, dialect string
}

// dialectRows is a dialect's rows at a minor, and whether the table has rows
// for that dialect (oas.Spec.SchemaRules).
type dialectRows struct {
	rules oas.SchemaRules
	read  bool
}

// of returns the version a document declaring declared is held to, its minor,
// and the rows its schemas are read with where none names a dialect of its
// own: those of dialect, its jsonSchemaDialect.
func (d *dialects) of(declared, dialect string) (version, minor string, rules oas.SchemaRules) {
	version, minor = d.version, d.minor
	if m, ok := SupportedMinor(declared); ok {
		version, minor = declared, m
	}
	return version, minor, d.at(version, dialect).rules
}

// at returns dialect's rows at the minor version names.
func (d *dialects) at(version, dialect string) dialectRows {
	minor, _ := SupportedMinor(version)
	k := dialectKey{minor: minor, dialect: dialect}
	if r, built := d.rules[k]; built {
		return r
	}
	if d.rules == nil {
		d.rules = map[dialectKey]dialectRows{}
	}
	rules, read := d.spec.SchemaRules(version, dialect)
	d.rules[k] = dialectRows{rules: rules, read: read}
	return d.rules[k]
}

// Dialects is the rows the schemas held by a schema naming its dialect
// (bind.Document.Dialects) are read with, by each node of that schema's tree.
// A schema no such schema holds reads its document's rows.
type Dialects struct {
	byNode map[*yaml.Node]oas.SchemaRules
}

// Of returns the rows the schema whose node is n is read with where a schema
// holding it, itself included, names its dialect, and false where none does.
// A node an alias places under two such schemas reads the first's, as the
// documents and their positions sort.
func (d Dialects) Of(n *yaml.Node) (oas.SchemaRules, bool) {
	r, ok := d.byNode[nodeview.Deref(n)]
	return r, ok
}

// govern notes that every node of the tree under root, which aliases place
// there included, reads rules, but those a schema before it governs already.
// Each node is read once, so the walk is bounded by the document's nodes.
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
// table read names, and an openapi/unread-dialect warning for each dialect the
// table has no rows for, whose schemas read their minor's own: at each such
// `$schema`, placed by place, and at each document's jsonSchemaDialect, the
// source's, sourceDialect, among them. A document two names read is warned of
// once.
func readDialects(table *bind.Table, others Others, d *dialects, sourceDialect string,
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
	at := ids.Ptr("jsonSchemaDialect")
	if sourceDialect != "" && !d.at(d.version, sourceDialect).read {
		unread(place(bind.Position{Doc: bind.Source, Pointer: at}), "jsonSchemaDialect", sourceDialect)
	}
	for _, o := range others.List() {
		if o.dialect != "" && !d.at(o.version, o.dialect).read {
			unread(ir.Provenance{Source: o.Index, Pointer: at}, "jsonSchemaDialect", o.dialect)
		}
	}
	out := Dialects{byNode: map[*yaml.Node]oas.SchemaRules{}}
	for id := range bind.DocID(table.Documents()) {
		version, ok := versionOf(table, others, d, id)
		if !ok {
			continue
		}
		for _, root := range table.Document(id).Dialects {
			r := d.at(version, root.URI)
			if !r.read {
				unread(place(bind.Position{Doc: id, Pointer: root.At.Pointer.AppendToken("$schema")}), "$schema", root.URI)
			}
			out.govern(root.Node, r.rules)
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
