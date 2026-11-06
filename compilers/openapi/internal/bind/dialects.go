// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// DialectRoot is a schema resource root whose `$schema` names its dialect: a
// schema no other holds, or one whose identifier roots a resource of its own
// (enterIn); where it is, its node, and the URI its `$schema` names. OAS 3.1
// has a `$schema` choose the dialect of the resource it roots
// (docs/compilers/openapi.md#dialect-keywords).
type DialectRoot struct {
	Node *yaml.Node
	URI  string
	At   Position
}

// heldBySchema reports whether a schema holds the position tokens reach from
// a document's root read at s, the state the document is identified by: a
// position below a Schema Object, or below the root of a document that is no
// OpenAPI document, which holds every schema in it. The root of a resource an
// $id embeds is held so, and roots a resource all the same.
func heldBySchema(s state, tokens []string) bool {
	for _, token := range tokens {
		if s == sSchema || s == sLoose {
			return true
		}
		s = step(s, token)
	}
	return false
}

// dialectsByDocument returns the dialect roots of roots by the document each
// is in, each document's in pointer order.
func dialectsByDocument(roots map[Position]DialectRoot) map[DocID][]DialectRoot {
	out := map[DocID][]DialectRoot{}
	for _, r := range roots {
		out[r.At.Doc] = append(out[r.At.Doc], r)
	}
	for _, list := range out {
		slices.SortFunc(list, func(a, b DialectRoot) int { return comparePositions(a.At, b.At) })
	}
	return out
}

// stepIn is step for a token of the mapping n of document id, read at s in
// dialect, which a dialect the drafts before 2020-12 spell reads otherwise
// (dialectStep), at a schema or at a document of schemas' root alike. Every
// child a walk reads asks it, so the other tokens take step without a call
// more.
func stepIn(d *docs, id DocID, n *yaml.Node, s state, token, dialect string) state {
	if s == sSchema || s == sLoose {
		switch token {
		case "items", "additionalItems", "definitions", "dependencies":
			return dialectStep(d, id, n, s, token, dialect)
		}
	}
	return step(s, token)
}

// dialectStep is stepIn for a schema's token a dialect reads otherwise
// (DialectRule): where it has `items` written as a list be a tuple, that list
// holds schemas and the `additionalItems` beside it holds its rest, and where
// it has `definitions`, or `dependencies`, hold schemas, they do, a property's
// list of names among the latter's entries holding none.
func dialectStep(d *docs, id DocID, n *yaml.Node, s state, token, dialect string) state {
	switch token {
	case "definitions":
		if dialectRule(d, id, dialect).Definitions {
			return sSchemaMap
		}
	case "dependencies":
		if dialectRule(d, id, dialect).Dependencies {
			return sSchemaMap
		}
	case "items":
		if listsItems(d, id, n) && dialectRule(d, id, dialect).Tuples {
			return sSchemaList
		}
	case "additionalItems":
		if listsItems(d, id, n) && dialectRule(d, id, dialect).Tuples {
			return sSchema
		}
	}
	return step(s, token)
}

// listsItems reports whether the schema mapping n writes `items` as a list, a
// tuple, in document id.
func listsItems(d *docs, id DocID, n *yaml.Node) bool {
	items := nodeview.Deref(d.list[id].view.ChildByToken(nodeview.Deref(n), "items"))
	return items != nil && items.Kind == yaml.SequenceNode
}

// entering is what reading a schema enters: the resource its content is in,
// whether it roots that resource, whether that is known (resources.enter), the
// dialect its content is read in, and whether its `$schema` names that dialect.
type entering struct {
	res                  resource
	rooted, known, names bool
	dialect              string
}

// enterIn returns what the schema n at at enters, held in the resource in, its
// parent read in enclosing and, where schemaHeld says so, held by a schema
// (heldBySchema). A schema no other holds is read in the dialect its `$schema`
// names, which spells its identifier. One a schema holds roots a resource
// where it writes an identifier as its embedder reads one (enter), as 2020-12
// has an embedded resource do (§9.3.1), or as its `$schema`'s dialect spells
// one beside no $ref its embedder ignores, and only then is read in that
// dialect. A `$schema` naming no dialect (DialectRule.NamesNone) chooses none.
func (r resources) enterIn(in resource, at Position, n *yaml.Node, enclosing string, schemaHeld func() bool) entering {
	doc := at.Doc
	uri, written := scalarOf(r.docs.list[doc].view.ChildByToken(nodeview.Deref(n), "$schema"))
	named := written && !dialectRule(r.docs, doc, uri).NamesNone
	held := named && schemaHeld()
	own := dialectRule(r.docs, doc, enclosing)
	if named && !held {
		own = dialectRule(r.docs, doc, uri)
	}
	res, rooted, known := r.enter(in, n, own)
	switch {
	case !named:
		return entering{res: res, rooted: rooted, known: known, dialect: enclosing}
	case !held:
		return entering{res: res, rooted: rooted, known: known, names: true, dialect: uri}
	}
	spelled := dialectRule(r.docs, doc, uri)
	if !rooted && spelled.Keyword != own.Keyword && !r.ignoresIdentifier(doc, n, own) {
		res, rooted, known = r.enter(in, n, spelled)
	}
	if !rooted {
		return entering{res: res, known: known, dialect: enclosing}
	}
	return entering{res: res, rooted: true, known: known, names: true, dialect: uri}
}

// heldAt reports whether a schema holds the position at (heldBySchema).
func heldAt(d *docs, at Position) bool {
	return heldBySchema(d.identityState(at.Doc), slices.Collect(at.Pointer.Tokens()))
}

// DialectAt returns the innermost of roots, a dialect root by where it is, at
// p or holding it in p's document, and false where none does: the root whose
// `$schema` names the dialect the schema at p is read in. A position reads it
// by where it is, not by its node, so a node an alias places under two roots
// reads each one's at each.
func DialectAt[V any](roots map[Position]V, p Position) (V, bool) {
	for ptr := p.Pointer; len(roots) > 0; ptr = ptr.Parent() {
		if r, ok := roots[Position{Doc: p.Doc, Pointer: ptr}]; ok {
			return r, true
		}
		if ptr == "" {
			break
		}
	}
	var none V
	return none, false
}

// dialectAbove returns the dialect the schema at p is read in, which a walk
// from p starts in: that of the dialect root at or above it the documents
// declare (identifiers.dialects), or its document's default.
func dialectAbove(d *docs, ix *identifiers, p Position) string {
	if r, ok := DialectAt(ix.dialects, p); ok {
		return r.URI
	}
	return defaultDialect(d, p.Doc)
}

// rootDialect returns the dialect the root at p names, where one is there,
// and inherited, that of the schema holding it, otherwise.
func rootDialect(ix *identifiers, p Position, inherited string) string {
	if root, ok := ix.dialects[p]; ok {
		return root.URI
	}
	return inherited
}

// defaultDialect returns the dialect the schemas of document id name none of
// their own are read in: an OpenAPI document's jsonSchemaDialect, or "".
func defaultDialect(d *docs, id DocID) string {
	if !d.list[id].openapi {
		return ""
	}
	uri, _ := scalarOf(d.list[id].view.ChildByToken(d.list[id].root, "jsonSchemaDialect"))
	return uri
}

// dialectRule returns how the table reads a schema of document id read in
// dialect (Config.Dialect), the keyword `$id` where the configuration says
// none. Every schema asks it, so each document's answer for a dialect is
// remembered, and the last one asked, nearly every asking's, kept to hand.
func dialectRule(d *docs, id DocID, dialect string) DialectRule {
	k := ruleKey{doc: id, dialect: dialect}
	if d.last.key == k && d.last.asked {
		return d.last.rule
	}
	rule, asked := d.rules[k]
	if !asked {
		if d.cfg.Dialect != nil {
			rule = d.cfg.Dialect(d.list[id].name, dialect)
		}
		if rule.Keyword == "" {
			rule.Keyword = "$id"
		}
		d.rules[k] = rule
	}
	d.last.key, d.last.rule, d.last.asked = k, rule, true
	return rule
}
