// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"maps"
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
	At   DocumentPointer
}

// heldBySchema reports whether a schema holds the position tokens reach from a
// document's root read at stateValue, the state the document is identified by:
// a position below a Schema Object, or below the root of a document that is no
// OpenAPI document, which holds every schema in it. The root of a resource an
// $id embeds is held so, and roots a resource all the same.
func heldBySchema(stateValue state, tokens []string) bool {
	for _, token := range tokens {
		if stateValue == sSchema || stateValue == sLoose {
			return true
		}
		stateValue = step(stateValue, token)
	}
	return false
}

// dialectsByDocument returns the dialect roots of roots by the document each
// is in, each document's in pointer order.
func dialectsByDocument(roots map[DocumentPointer]DialectRoot) map[DocID][]DialectRoot {
	out := map[DocID][]DialectRoot{}
	for _, dialectRoot := range roots {
		out[dialectRoot.At.Doc] = append(out[dialectRoot.At.Doc], dialectRoot)
	}
	for _, list := range out {
		slices.SortFunc(list, func(a, b DialectRoot) int { return comparePositions(a.At, b.At) })
	}
	return out
}

// stepIn is step for a token of the mapping node of document id, read at
// stateValue in dialect, which a dialect the drafts before 2020-12 spell reads
// otherwise (dialectStep), at a schema or at a document of schemas' root alike.
// Every child a walk reads asks it, so the other tokens take step without a
// call more.
func stepIn(docsValue *docs, id DocID, node *yaml.Node, stateValue state, token, dialect string) state {
	if stateValue == sSchema || stateValue == sLoose {
		switch token {
		case "items", "additionalItems", "definitions", "dependencies":
			return dialectStep(docsValue, id, node, stateValue, token, dialect)
		}
	}
	return step(stateValue, token)
}

// dialectStep is stepIn for a schema's token a dialect reads otherwise
// (DialectRule): where it has `items` written as a list be a tuple, that list
// holds schemas and the `additionalItems` beside it holds its rest, and where
// it has `definitions`, or `dependencies`, hold schemas, they do, a property's
// list of names among the latter's entries holding none.
func dialectStep(docsValue *docs, id DocID, node *yaml.Node, stateValue state, token, dialect string) state {
	switch token {
	case "definitions":
		if dialectRule(docsValue, id, dialect).Definitions {
			return sSchemaMap
		}
	case "dependencies":
		if dialectRule(docsValue, id, dialect).Dependencies {
			return sSchemaMap
		}
	case "items":
		if listsItems(docsValue, id, node) && dialectRule(docsValue, id, dialect).Tuples {
			return sSchemaList
		}
	case "additionalItems":
		if listsItems(docsValue, id, node) && dialectRule(docsValue, id, dialect).Tuples {
			return sSchema
		}
	}
	return step(stateValue, token)
}

// listsItems reports whether the schema mapping node writes `items` as a list,
// a tuple, in document id.
func listsItems(docsValue *docs, id DocID, node *yaml.Node) bool {
	items := nodeview.Deref(docsValue.list[id].view.ChildByToken(nodeview.Deref(node), "items"))
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

// enterIn returns what the schema node at at enters, held in the resource in,
// its parent read in enclosing and, where schemaHeld says so, held by a schema
// (heldBySchema). A schema no other holds is read in the dialect its `$schema`
// names, which spells its identifier. One a schema holds roots a resource where
// it writes an identifier as its embedder reads one (enter), as 2020-12 has an
// embedded resource do (§9.3.1), or as its `$schema`'s dialect spells one
// beside no $ref its embedder ignores, and only then is read in that dialect. A
// `$schema` naming no dialect (DialectRule.NamesNone) chooses none.
func (r resources) enterIn(in resource, at DocumentPointer, node *yaml.Node, enclosing string, schemaHeld func() bool) entering {
	doc := at.Doc
	uri, written := scalarOf(r.docs.list[doc].view.ChildByToken(nodeview.Deref(node), "$schema"))
	named := written && !dialectRule(r.docs, doc, uri).NamesNone
	held := named && schemaHeld()
	own := dialectRule(r.docs, doc, enclosing)
	if named && !held {
		own = dialectRule(r.docs, doc, uri)
	}
	res, rooted, known := r.enter(in, node, own)
	switch {
	case !named:
		return entering{res: res, rooted: rooted, known: known, dialect: enclosing}
	case !held:
		return entering{res: res, rooted: rooted, known: known, names: true, dialect: uri}
	}
	spelled := dialectRule(r.docs, doc, uri)
	if !rooted && spelled.Keyword != own.Keyword && !r.ignoresIdentifier(doc, node, own) {
		res, rooted, known = r.enter(in, node, spelled)
	}
	if !rooted {
		return entering{res: res, known: known, dialect: enclosing}
	}
	return entering{res: res, rooted: true, known: known, names: true, dialect: uri}
}

// heldAt reports whether a schema holds the position at (heldBySchema).
func heldAt(docsValue *docs, at DocumentPointer) bool {
	return heldBySchema(docsValue.identityState(at.Doc), slices.Collect(at.Pointer.Tokens()))
}

// DialectAt returns the innermost of roots, a dialect root by where it is, at
// documentPointer or holding it in documentPointer's document, and false where
// none does: the root whose `$schema` names the dialect the schema at
// documentPointer is read in. A position reads it by where it is, not by its
// node, so a node an alias places under two roots reads each one's at each.
func DialectAt[V any](roots map[DocumentPointer]V, documentPointer DocumentPointer) (V, bool) {
	for ptr := documentPointer.Pointer; len(roots) > 0; ptr = ptr.Parent() {
		if r, ok := roots[DocumentPointer{Doc: documentPointer.Doc, Pointer: ptr}]; ok {
			return r, true
		}
		if ptr == "" {
			break
		}
	}
	var none V
	return none, false
}

// dialectAbove returns the dialect the schema at documentPointer is read in,
// which a walk from documentPointer starts in: that of the dialect root at or
// above it the documents declare (identifiers.dialects), or its document's
// default.
func dialectAbove(docsValue *docs, ix *identifiers, documentPointer DocumentPointer) string {
	if dialectRoot, ok := DialectAt(ix.dialects, documentPointer); ok {
		return dialectRoot.URI
	}
	return defaultDialect(docsValue, documentPointer.Doc)
}

// rootDialect returns the dialect the root at documentPointer names, where one
// is there, and inherited, that of the schema holding it, otherwise.
func rootDialect(ix *identifiers, documentPointer DocumentPointer, inherited string) string {
	if root, ok := ix.dialects[documentPointer]; ok {
		return root.URI
	}
	return inherited
}

// defaultDialect returns the dialect the schemas of document id name none of
// their own are read in: an OpenAPI document's jsonSchemaDialect, or "".
func defaultDialect(docsValue *docs, id DocID) string {
	if !docsValue.list[id].openapi {
		return ""
	}
	uri, _ := scalarOf(docsValue.list[id].view.ChildByToken(docsValue.list[id].root, "jsonSchemaDialect"))
	return uri
}

// dialectRule returns how the table reads a schema of document id read in
// dialect (Config.Dialect), the keyword `$id` where the configuration says
// none. Every schema asks it, so each document's answer for a dialect is
// remembered, and the last one asked, nearly every asking's, kept to hand.
func dialectRule(docsValue *docs, id DocID, dialect string) DialectRule {
	ruleKeyValue := ruleKey{doc: id, dialect: dialect}
	if docsValue.last.key == ruleKeyValue && docsValue.last.asked {
		return docsValue.last.rule
	}
	rule, asked := docsValue.rules[ruleKeyValue]
	if !asked {
		if docsValue.cfg.Dialect != nil {
			rule = docsValue.cfg.Dialect(docsValue.list[id].name, dialect)
		}
		if rule.Keyword == "" {
			rule.Keyword = "$id"
		}
		docsValue.rules[ruleKeyValue] = rule
	}
	docsValue.last.key, docsValue.last.rule, docsValue.last.asked = ruleKeyValue, rule, true
	return rule
}

// landingRoot returns the dialect root a landing at documentPointer, holding
// node, is, where it is one: a schema the identity walk placed no schema at or
// above, content no grammar reads, writing a `$schema` naming a dialect, that
// no other landing's schema holds, so the `$schema` chooses the dialect of what
// it holds (docs/compilers/openapi.md#dialect-keywords). Whether one holds it
// turns on the landings alone, so no order of reading decides it.
func landingRoot(finderValue *finder, documentPointer DocumentPointer, node *yaml.Node) (DialectRoot, bool) {
	if !finderValue.landings[documentPointer] || heldBySchema(finderValue.docs.identityState(documentPointer.Doc), slices.Collect(documentPointer.Pointer.Tokens())) {
		return DialectRoot{}, false
	}
	for above := documentPointer.Pointer; above != ""; {
		above = above.Parent()
		if finderValue.landings[DocumentPointer{Doc: documentPointer.Doc, Pointer: above}] {
			return DialectRoot{}, false
		}
	}
	node = nodeview.Deref(node)
	uri, written := scalarOf(finderValue.docs.list[documentPointer.Doc].view.ChildByToken(node, "$schema"))
	if !written || dialectRule(finderValue.docs, documentPointer.Doc, uri).NamesNone {
		return DialectRoot{}, false
	}
	return DialectRoot{At: documentPointer, URI: uri, Node: node}, true
}

// startDialect is the dialect a walk from documentPointer, holding node, starts
// in: the one documentPointer roots as a landing (landingRoot), else the one
// above it (dialectAbove).
func startDialect(finderValue *finder, documentPointer DocumentPointer, node *yaml.Node) string {
	if root, ok := landingRoot(finderValue, documentPointer, node); ok {
		return root.URI
	}
	return dialectAbove(finderValue.docs, finderValue.ids, documentPointer)
}

// withLandingRoots returns roots with each landing finderValue found that is a
// dialect root of its own (landingRoot) added, roots itself where none is. Each
// landing's node is read as the walk that found it read it, charged then.
func withLandingRoots(finderValue *finder, roots map[DocumentPointer]DialectRoot) map[DocumentPointer]DialectRoot {
	out := roots
	for at := range finderValue.landings {
		doc := finderValue.docs.list[at.Doc]
		path, _ := doc.view.DocumentPath(doc.root, at.Pointer)
		if root, ok := landingRoot(finderValue, at, path[len(path)-1]); ok {
			if len(out) == len(roots) {
				out = make(map[DocumentPointer]DialectRoot, len(roots)+1)
				maps.Copy(out, roots)
			}
			out[at] = root
		}
	}
	return out
}
