// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package naming

import (
	"encoding/json/jsontext"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
)

// componentSchemaTokens is the length of /components/schemas/<name>, the
// pointer a component schema's walk is rooted at.
const componentSchemaTokens = 3

// owner returns the hint of the object documentPointer belongs to and how many of documentPointer's
// tokens lead to its schema, kinds being how the grammar reads the way to documentPointer
// (reftable.ReferenceTargetTable.Kinds): the schema whose structure holds documentPointer
// (structureRoot), named for what holds it there (ownerOf) or, where that
// schema is a document's root, for the document (Document). A structure
// nothing names an owner of, such as one only a $ref reaches under an x- key,
// is named from its pointer alone (pointerOwner).
func (n *Namer) owner(documentPointer reftable.DocumentPointer, tokens []string, kinds []reftable.Kind) (int, string) {
	if n.table == nil {
		return pointerOwner(tokens)
	}
	root, ok := structureRoot(kinds)
	switch {
	case !ok:
		return pointerOwner(tokens)
	case root == 0:
		return 0, n.Document(documentPointer.Doc)
	}
	if hint, ok := n.ownerOf(documentPointer.Doc, tokens, kinds, root-1); ok {
		return root, hint
	}
	return pointerOwner(tokens)
}

// structureRoot returns the index in kinds of the schema whose structure
// holds the last position: the innermost schema on the way, which holds what
// lies past it, such as an enum literal, then the topmost schema above it that
// no position outside a structure (inStructure) separates it from. An object
// a reference reads inside a schema's raw YAML, such as a parameter kept
// under its x- key, so starts a structure of its own. ok is false where no
// schema is on the way.
func structureRoot(kinds []reftable.Kind) (int, bool) {
	root := -1
	for i, kind := range kinds {
		if kind == reftable.KindSchema {
			root = i
		}
	}
	if root < 0 {
		return 0, false
	}
	for i := root - 1; i >= 0 && inStructure(kinds[i]); i-- {
		if kinds[i] == reftable.KindSchema {
			root = i
		}
	}
	return root, true
}

// inStructure reports whether a position of kind kind can lie inside a schema's
// structure: a schema, or a map or a list of them, a list being
// reftable.KindOther. That kind covers objects such as a path item too, but each
// holds a schema only through an owner, which ends a structure as raw YAML
// does.
func inStructure(kind reftable.Kind) bool {
	return kind == reftable.KindSchema || kind == reftable.KindSchemaMap || kind == reftable.KindOther
}

// Document is the hint what is declared at the root of the document doc is
// named by, which no key names: the document's name, what follows its last
// '/' (refHint), as a $ref spelling the document suggests it. A schema there
// is named so, as is a Security Scheme Object a requirement key lands on. doc
// is one the table holds, as only a reference the table read lands there.
func (n *Namer) Document(doc reftable.DocID) string {
	return refHint(n.table.Document(doc).Name)
}

// ownerOf returns the hint of the schema whose key is tokens[i], held at the
// position tokens[:i] leads to: a component schema's key, the owner of the
// content a media type's schema or itemSchema belongs to, or the key of the
// components/mediaTypes entry declaring it, a parameter's name and a header's
// key. ok is false for a schema nothing names an owner of.
func (n *Namer) ownerOf(doc reftable.DocID, tokens []string, kinds []reftable.Kind, i int) (string, bool) {
	holder := pointerOf(tokens[:i])
	switch kinds[i] {
	case reftable.KindSchemaMap:
		return tokens[i], i > 0 && kinds[i-1] == reftable.KindComponents
	case reftable.KindParameter:
		return n.parameterName(doc, holder), true
	case reftable.KindHeader:
		return ids.DeclarationHint(holder, holder.LastToken()), true
	case reftable.KindMediaType:
		hint, ok := n.mediaTypeOwner(doc, holder, kinds[:i])
		if ok && tokens[i] == "itemSchema" {
			return compile.SubHint(hint, "item"), true
		}
		return hint, ok
	default:
		return "", false
	}
}

// mediaTypeOwner returns the hint of the owner of the media type at holder,
// above which kinds lie: a components/mediaTypes entry is its own, by its key,
// the empty key included, as a component schema keyed "" is named, and any
// other is keyed under its owner's content, holder being
// <owner>/content/<media type> (contentOwner).
func (n *Namer) mediaTypeOwner(doc reftable.DocID, holder jsontext.Pointer, kinds []reftable.Kind) (string, bool) {
	if section, name, ok := ids.ComponentKey(holder); ok && section == "mediaTypes" {
		return name, true
	}
	if len(kinds) < 2 {
		return "", false
	}
	return n.contentOwner(doc, holder.Parent().Parent(), kinds[len(kinds)-2])
}

// contentOwner returns the hint of the object at at, of kind kind, whose content
// a media type's schema is: a request body (requestBodyName), a response by
// its component key, else "response", a parameter by its name and a header by
// its key.
func (n *Namer) contentOwner(doc reftable.DocID, at jsontext.Pointer, kind reftable.Kind) (string, bool) {
	switch kind {
	case reftable.KindRequestBody:
		return n.requestBodyName(doc, at), true
	case reftable.KindResponse:
		return ids.DeclarationHint(at, "response"), true
	case reftable.KindParameter:
		return n.parameterName(doc, at), true
	case reftable.KindHeader:
		return ids.DeclarationHint(at, at.LastToken()), true
	default:
		return "", false
	}
}

// elsewhereBody names a request body no operationId names: one an operation
// without an operationId declares, or one no operation declares.
const elsewhereBody = "request"

// requestBodyName is the hint the request body written at at is named by: its
// component's key, else `<operationId>_request` for the operation it is
// written in, else elsewhereBody. It is a function of where the body is
// written, never of which operation mounts it (GitHub #433, #107).
func (n *Namer) requestBodyName(doc reftable.DocID, at jsontext.Pointer) string {
	if id := n.text(doc, at.Parent(), "operationId"); id != "" {
		return ids.DeclarationHint(at, id+"_request")
	}
	return ids.DeclarationHint(at, elsewhereBody)
}

// parameterName is the name the parameter written at at declares.
func (n *Namer) parameterName(doc reftable.DocID, at jsontext.Pointer) string {
	return n.text(doc, at, "name")
}

// text returns the scalar the mapping written at at holds under field, as the
// parser reads a string field (scalarText).
func (n *Namer) text(doc reftable.DocID, at jsontext.Pointer, field string) string {
	return scalarText(nodeview.Child(n.node(doc, at), field))
}

// scalarText returns the text of v as the parser's model reads a string field:
// a string, a number or a boolean as written, and nothing for null, a node
// that is no scalar, or a scalar under any other tag, such as !!binary or a
// local one (docs/parser-library.md#undecoded-scalar).
func scalarText(v *yaml.Node) string {
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	switch v.ShortTag() {
	case "!!str", "!!int", "!!float", "!!bool":
		return v.Value
	default:
		return ""
	}
}

// pointerOwner names a position from its pointer alone: beneath
// /components/schemas/<name>, from that name; elsewhere from its first token,
// the name a position only references reach takes.
func pointerOwner(tokens []string) (int, string) {
	start := min(1, len(tokens))
	if len(tokens) >= componentSchemaTokens && tokens[0] == "components" && tokens[1] == "schemas" {
		start = componentSchemaTokens
	}
	if start == 0 {
		return 0, ""
	}
	return start, tokens[start-1]
}

// pointerOf joins tokens into a pointer.
func pointerOf(tokens []string) jsontext.Pointer {
	return ids.Ptr(tokens...)
}
