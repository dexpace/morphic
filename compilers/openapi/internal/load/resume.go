package load

import (
	"encoding/json/jsontext"
	"errors"
	"strings"

	"github.com/speakeasy-api/openapi/jsonpointer"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// resumable reports whether settle may resume r, whose resolution holds c and
// failed with err. Only a failure mending cures is resumed: a hop that reached
// a stand-in for the source's object (see external.holdObject), or a pointer
// walked through a document's bytes (GitHub #761), where a tree prepared from
// them can take their place. A cycle is reported as the library reports it, so
// its message follows the walk's order (GitHub #767). The stalled hop read its
// own record when r is a schema, and the previous hop's otherwise; a schema is
// resumed only where its remaining hops end (see ends).
func (e external) resumable(r resolvable, c chain, err error) bool {
	if e.read.stoodIn(c) {
		return true
	}
	if !errors.Is(err, jsonpointer.ErrInvalidPath) || c.cut || len(c.records) == 0 ||
		c.stopped == "" || c.stopped.GetURI() != "" {
		return false
	}
	last := c.records[len(c.records)-1]
	_, schema := r.(*schemaRef)
	data, isBytes := (*last.document).([]byte)
	if !isBytes || schema == last.reached {
		return false
	}
	tree := e.read.treeFor(last.path, data)
	return tree != nil && (!schema || ends(tree, c.stopped))
}

// ends reports whether the hops a schema chain has left from ref end in the
// document tree holds: each $ref followed to a node carrying none, or to no
// node, within maxResolutionHops. A cycle never ends.
//
// The library restarts its cycle check at each schema hop it resolved before,
// so a resumed chain leading back through one recurses until the stack
// overflows, which nothing recovers from. A hop this walk cannot read as the
// library does is no answer (see withinDocument): a chain leaving the document
// is not resumed, and fails as it would without settle.
func ends(tree *yaml.Node, ref references.Reference) bool {
	view := nodeview.New()
	return endsWithin(ref, func(ref references.Reference) (references.Reference, hopKind) {
		pointer, ok := withinDocument(ref)
		if !ok {
			return "", hopUnread
		}
		return readHop(view, tree, pointer)
	})
}

// hopKind is what reading one hop of a chain found.
type hopKind int

const (
	hopEnds    hopKind = iota // the target carries no $ref, or there is no target
	hopFollows                // the target carries a $ref, which the read returns
	hopUnread                 // the walk cannot read the hop as the resolver does
	// hopPending is hopEnds for now: the pointer passes a reference the walk
	// has not resolved yet, which the model reads as no target until it does.
	hopPending
)

// endsWithin follows the chain from ref, reading each hop with read, and
// reports whether it ends within maxResolutionHops. A cycle never ends, nor
// does a hop the walk cannot read.
func endsWithin(ref references.Reference, read func(references.Reference) (references.Reference, hopKind)) bool {
	for range maxResolutionHops {
		next, found := read(ref)
		switch found {
		case hopEnds, hopPending:
			return true
		case hopUnread:
			return false
		default:
			ref = next
		}
	}
	return false
}

// readHop reads the hop naming pointer in document with the call the resolver
// makes, so it finds the target the resolver finds, and returns the $ref the
// target carries. A target that is no schema is one the resolver fails to
// cast, so the chain stops there.
func readHop(view *nodeview.View, document any, pointer string) (references.Reference, hopKind) {
	target, err := jsonpointer.GetTarget(document, jsonpointer.JSONPointer(pointer), jsonpointer.WithStructTags("key"))
	if err != nil {
		return "", settledRead(err)
	}
	var next string
	switch t := target.(type) {
	case *yaml.Node:
		next = refOf(view, t)
	case *schemaRef:
		if t.IsReference() {
			next = string(t.GetRef())
		}
	default:
		// Not a schema, so it carries no $ref the resolver follows.
	}
	if next == "" {
		return "", hopEnds
	}
	return references.Reference(next), hopFollows
}

// settledRead is what a read that failed with err found: no target, unless the
// pointer passes a reference the walk has not resolved yet, which the model
// refuses to read through until it does, and which a later read passes.
func settledRead(err error) hopKind {
	if errors.Is(err, jsonpointer.ErrNotFound) || errors.Is(err, jsonpointer.ErrInvalidPath) ||
		errors.Is(err, jsonpointer.ErrValidation) {
		return hopEnds
	}
	return hopPending
}

// withinDocument returns the pointer ref names in the document holding it,
// decoded as the library decodes it, or false for a reference the library
// reads otherwise: one naming a document, which may be read for the first time
// or resolved through a $id, an anchor, and a "#/$defs/..." pointer, which it
// may read relative to the schema spelling it.
func withinDocument(ref references.Reference) (string, bool) {
	pointer := string(ref.GetJSONPointer())
	if ref.GetURI() != "" || !strings.HasPrefix(pointer, "/") || defs.IsPointer(jsontext.Pointer(pointer)) {
		return "", false
	}
	return pointer, true
}

// refOf returns the $ref node carries as the library reads it, through an
// alias or a merge key, or "" for none or one that is not a string.
func refOf(view *nodeview.View, node *yaml.Node) string {
	v := nodeview.Deref(view.ChildByToken(nodeview.Deref(node), "$ref"))
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// withoutStall returns c less the record of the hop it stalled at, which a
// schema hop holds of its own. That record stays as the library wrote it
// unless the hop is resumed: a later resolution can reach the hop through hops
// resolved before, across which the library checks for no cycle, and a mended
// record would let it take the chain up past the stall unchecked.
func (c chain) withoutStall() chain {
	if n := len(c.records); n > 0 && !c.records[n-1].reached {
		c.records = c.records[:n-1]
	}
	return c
}
