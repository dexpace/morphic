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
// (bind.Document.Dialects) are read with: by where each is (At), and by each
// node of that root's tree (Of), a root it holds governing its own. A schema
// no such root holds reads its document's rows. Nothing writes it once the
// load phase returns it, so every context copying it may read it at once.
type Dialects struct {
	byRoot map[bind.Position]oas.SchemaRules
	byNode map[*yaml.Node]oas.SchemaRules
	// byDoc is the rows each document's schemas no root governs are read
	// with, and byName each document by its name, for the load phase's own
	// judgement (ownedFindings).
	byDoc  map[bind.DocID]oas.SchemaRules
	byName map[string]bind.DocID
}

// At returns the rows the schema at p is read with where a root at or above
// it names its dialect, the innermost such root's, and false where none does.
func (d Dialects) At(p bind.Position) (oas.SchemaRules, bool) {
	return bind.DialectAt(d.byRoot, p)
}

// rowsAt returns the rows the schema at p is read with: the innermost dialect
// root's at or above it (At), else its document's, else the source's, as for a
// document the compile did not read beside it.
func (d Dialects) rowsAt(p bind.Position) oas.SchemaRules {
	if r, ok := d.At(p); ok {
		return r
	}
	if r, ok := d.byDoc[p.Doc]; ok {
		return r
	}
	return d.byDoc[bind.Source]
}

// Of is At for a reading that has the schema's node and not where it is. A
// node an alias places at several positions reads at all of them the rows of
// the root last in pointer order among those whose trees hold it, as
// readDialects governs them, so only a reading no valid node answers otherwise
// in another dialect asks it, as an exclusive bound valid in draft 4 is
// malformed in 2020-12.
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
// jsonSchemaDialect, the source's among them. A value no absolute URI names
// chooses nothing and draws none: validation reports it where it judges it,
// which in a document a reference reaches is only what the reference reaches.
// A document two names read is warned of once.
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
	out := Dialects{byRoot: map[bind.Position]oas.SchemaRules{}, byNode: map[*yaml.Node]oas.SchemaRules{},
		byDoc: map[bind.DocID]oas.SchemaRules{bind.Source: d.at(d.version, d.dialect).rows.Schema}, byName: map[string]bind.DocID{}}
	for id := range bind.DocID(table.Documents()) {
		out.byName[table.Document(id).Name] = id
		version, ok := versionOf(table, others, d, id)
		if !ok {
			continue
		}
		if o, listed := others.Named(table.Document(id).Name); listed && id != bind.Source {
			out.byDoc[id] = d.at(o.version, o.dialect).rows.Schema
		}
		// Innermost first: a root's pointer extends that of a root holding it,
		// so it sorts after, and the list is in pointer order.
		for _, root := range slices.Backward(table.Document(id).Dialects) {
			if !oas.NamesDialect(root.URI) {
				// It chooses nothing: the schema reads as though it wrote none,
				// so a malformed value adds no reading to validation's finding.
				continue
			}
			r := d.at(version, root.URI)
			if r.unread {
				unread(place(bind.Position{Doc: id, Pointer: root.At.Pointer.AppendToken("$schema")}), "$schema", root.URI)
			}
			out.byRoot[root.At] = r.rows.Schema
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

// dialectRuleOf returns what the binder reads otherwise in the dialect rules
// are of than in 2020-12 (bind.DialectRule): the identifier under `$id`, or
// draft 4's `id` where the rows define it alone; an identifier naming a
// fragment alone as an anchor where they define no `$anchor`, as the drafts
// before 2019-09 have it; the drafts' plain-name grammar there, or where
// `$anchor` takes it; `definitions`, and schemas in `dependencies`, where they
// define them; and an `items` list as a tuple where they take one. Rows defining no identifier, as
// 3.0's, say nothing of it, and `$id` is read.
func dialectRuleOf(rules oas.SchemaRules) bind.DialectRule {
	rule := bind.DialectRule{Keyword: rules.Identifier(), Definitions: rules.Takes("definitions", oas.Map),
		Dependencies: rules.Defines("dependencies"), Tuples: rules.Takes("items", oas.List)}
	rule.Fragments = rule.Keyword != "" && !rules.Defines("$anchor")
	rule.PlainNames = rule.Fragments || rules.Formatted("$anchor", oas.PlainName)
	return rule
}
