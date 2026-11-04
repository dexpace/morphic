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

// dialectRows is a dialect's rows at a minor, and whether it is one the table
// has no rows for (oas.Spec.SchemaRules).
type dialectRows struct {
	rules  oas.SchemaRules
	unread bool
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
	rules, unread := d.spec.SchemaRules(version, dialect)
	d.rules[k] = dialectRows{rules: rules, unread: unread}
	return d.rules[k]
}

// Dialects is the rows the schemas a resource root naming its dialect holds
// (bind.Document.Dialects) are read with: by where each is (At), and by each
// node of that root's tree (Of), a root it holds governing its own. A schema
// no such root holds reads its document's rows.
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
// node an alias places at several positions reads one root's at all of them,
// so only a reading no valid node answers otherwise in another dialect asks it,
// as an exclusive bound valid in draft 4 is malformed in 2020-12.
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
// jsonSchemaDialect, the source's, sourceDialect, among them. Validation
// reports a value no absolute URI names. A document two names read is warned
// of once.
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
	at := jsontext.Pointer("").AppendToken("jsonSchemaDialect")
	if d.at(d.version, sourceDialect).unread {
		unread(place(bind.Position{Doc: bind.Source, Pointer: at}), "jsonSchemaDialect", sourceDialect)
	}
	for _, o := range others.List() {
		if d.at(o.version, o.dialect).unread {
			unread(ir.Provenance{Source: o.Index, Pointer: at}, "jsonSchemaDialect", o.dialect)
		}
	}
	out := Dialects{byRoot: map[bind.Position]oas.SchemaRules{}, byNode: map[*yaml.Node]oas.SchemaRules{},
		byDoc: map[bind.DocID]oas.SchemaRules{bind.Source: d.at(d.version, sourceDialect).rules}, byName: map[string]bind.DocID{}}
	for id := range bind.DocID(table.Documents()) {
		out.byName[table.Document(id).Name] = id
		version, ok := versionOf(table, others, d, id)
		if !ok {
			continue
		}
		if o, listed := others.Named(table.Document(id).Name); listed && id != bind.Source {
			out.byDoc[id] = d.at(o.version, o.dialect).rules
		}
		// Innermost first: a root's pointer extends that of a root holding it,
		// so it sorts after, and the list is in pointer order.
		for _, root := range slices.Backward(table.Document(id).Dialects) {
			r := d.at(version, root.URI)
			if r.unread {
				unread(place(bind.Position{Doc: id, Pointer: root.At.Pointer.AppendToken("$schema")}), "$schema", root.URI)
			}
			out.byRoot[root.At] = r.rules
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

// dialectRuleOf returns what the binder reads otherwise in the dialect rules
// are of than in 2020-12 (bind.DialectRule): a schema's identifier under
// `$id`, or under draft 4's `id` where the rows define it alone; an identifier
// naming a fragment alone as an anchor where they define one but no `$anchor`,
// as the drafts before 2019-09 spell one; `definitions` as a map of schemas
// where they define it; and `items` written as a list as a tuple where they
// take one. The rows of a minor that defines no identifier, as 3.0's, say
// nothing of it, and `$id` is read.
func dialectRuleOf(rules oas.SchemaRules) bind.DialectRule {
	rule := bind.DialectRule{Definitions: rules.Takes("definitions", oas.Map), Tuples: rules.Takes("items", oas.List)}
	switch {
	case rules.Defines("$id"):
		rule.Keyword = "$id"
	case rules.Defines("id"):
		rule.Keyword = "id"
	default:
		return rule
	}
	rule.Fragments = !rules.Defines("$anchor")
	return rule
}
