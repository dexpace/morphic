package naming

import (
	"encoding/json/jsontext"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// componentSchemaTokens is the length of /components/schemas/<name>, the
// pointer a component schema's walk is rooted at.
const componentSchemaTokens = 3

// owner returns the hint of the object p belongs to and how many of p's
// tokens lead to its schema: the first position on the way the grammar reads
// as a schema, named for what holds it there (ownerOf). A position whose
// first schema nothing names an owner of, such as one only a $ref reaches
// under an x- key, is named from its pointer alone (pointerOwner).
func (n *Namer) owner(p bind.Position, tokens []string) (int, string) {
	if n.table == nil {
		return pointerOwner(tokens)
	}
	kinds := n.table.Kinds(p)
	if kinds[0] == bind.KindSchema {
		// A document whose root a reference reads as a schema owns nothing
		// above it to name it after.
		return 0, ""
	}
	for i := range tokens {
		if i+1 >= len(kinds) || kinds[i+1] != bind.KindSchema {
			continue
		}
		if hint, ok := n.ownerOf(p.Doc, tokens, kinds, i); ok {
			return i + 1, hint
		}
		break
	}
	return pointerOwner(tokens)
}

// ownerOf returns the hint of the schema whose key is tokens[i], held at the
// position tokens[:i] leads to: a component schema's key, the owner of the
// content a media type's schema or itemSchema belongs to, a parameter's name
// and a header's key. ok is false for a schema nothing names an owner of.
func (n *Namer) ownerOf(doc bind.DocID, tokens []string, kinds []bind.Kind, i int) (string, bool) {
	holder := pointerOf(tokens[:i])
	switch kinds[i] {
	case bind.KindSchemaMap:
		return tokens[i], i > 0 && kinds[i-1] == bind.KindComponents
	case bind.KindParameter:
		return n.parameterName(doc, holder), true
	case bind.KindHeader:
		return ids.DeclarationHint(holder, holder.LastToken()), true
	case bind.KindMediaType:
		// The media type is keyed under its holder's content: holder is
		// <owner>/content/<media type>.
		if i < 2 {
			return "", false
		}
		hint, ok := n.contentOwner(doc, holder.Parent().Parent(), kinds[i-2])
		if ok && tokens[i] == "itemSchema" {
			return compile.SubHint(hint, "item"), true
		}
		return hint, ok
	default:
		return "", false
	}
}

// contentOwner returns the hint of the object at at, of kind k, whose content
// a media type's schema is: a request body (requestBodyName), a response by
// its component key, else "response", a parameter by its name and a header by
// its key.
func (n *Namer) contentOwner(doc bind.DocID, at jsontext.Pointer, k bind.Kind) (string, bool) {
	switch k {
	case bind.KindRequestBody:
		return n.requestBodyName(doc, at), true
	case bind.KindResponse:
		return ids.DeclarationHint(at, "response"), true
	case bind.KindParameter:
		return n.parameterName(doc, at), true
	case bind.KindHeader:
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
func (n *Namer) requestBodyName(doc bind.DocID, at jsontext.Pointer) string {
	if id := n.text(doc, at.Parent(), "operationId"); id != "" {
		return ids.DeclarationHint(at, id+"_request")
	}
	return ids.DeclarationHint(at, elsewhereBody)
}

// parameterName is the name the parameter written at at declares.
func (n *Namer) parameterName(doc bind.DocID, at jsontext.Pointer) string {
	return n.text(doc, at, "name")
}

// text returns the scalar the mapping written at at holds under field, as the
// parser reads a string field, null read as none.
func (n *Namer) text(doc bind.DocID, at jsontext.Pointer, field string) string {
	v := nodeview.Child(n.node(doc, at), field)
	if v == nil || v.Kind != yaml.ScalarNode || v.ShortTag() == "!!null" {
		return ""
	}
	return v.Value
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
