// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"net/url"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// resource is a schema resource (JSON Schema 2020-12 §8.2.1): the content of a
// document, rooted at its root and named by its name, or a schema whose $id
// names another URI than the resource holding it, rooted there and named by
// that URI. A reference written in one resolves against its name.
type resource struct {
	root Position
	uri  string
	// id reports whether an $id names it, so that its name is an identifier
	// rather than a document's location.
	id bool
	// detached reports that an $id on the way to it from its document's own
	// names an absolute URI or path, so a read against its base is judged by
	// the URI it resolves to rather than by its own relative spelling.
	detached bool
}

// resources reads the schema resources of the documents a table reads: where
// each begins, the URI naming it, and the base a reference written in one is
// read against.
type resources struct {
	docs *docs
}

// document is the resource of document id's own content.
func (r resources) document(id DocID) resource {
	return resource{root: Position{Doc: id}, uri: r.docs.list[id].name}
}

// identifiers is every identifier the documents' schemas declare: each URI an
// $id names, and each plain name an $anchor or a $dynamicAnchor declares, read
// off the node, as the model has no field for the last
// (docs/parser-library.md#unmodeled-keywords), by the root of the resource it is
// in. Only schemas are read (docs.identityState).
//
// A plain name is asked only of a document already open, so it answers once
// that document is read. A URI names no document, so one a document declares
// waits in pending for the pass to end, and no answer depends on which
// reference opened that document first (reread).
type identifiers struct {
	// resources is every resource read, each document's own among them, which
	// a walk's frame names by index; own is the index of each document's.
	resources []resource
	own       map[DocID]int32
	uris      map[string][]Position
	pending   map[string][]Position
	// asked is each URI a pass has asked for, whatever uris held.
	asked   map[string]bool
	anchors map[anchorKey][]Position
	// dynamic is every $dynamicAnchor by document and name, in any resource: a
	// reference expands only against the one declaration a document makes of
	// a name, so one in a resource of its own counts (docs/compilers/openapi.md#dynamic-ref).
	dynamic map[dynamicKey][]Position
	// truncated holds each document with a schema merging through `<<` deeper
	// than the view expands, so an identifier past the bound may be missing.
	truncated map[DocID]bool
	read      map[DocID]bool
}

// anchorKey is a plain name and the root of the resource declaring it.
type anchorKey struct {
	root Position
	name string
}

// dynamicKey is a $dynamicAnchor's name and the document declaring it.
type dynamicKey struct {
	doc  DocID
	name string
}

// newIdentifiers returns an index holding no identifier, whose first resource
// is source, the source document's own.
func newIdentifiers(source resource) *identifiers {
	return &identifiers{resources: []resource{source}, own: map[DocID]int32{Source: 0}, uris: map[string][]Position{},
		pending: map[string][]Position{}, asked: map[string]bool{}, anchors: map[anchorKey][]Position{},
		dynamic: map[dynamicKey][]Position{}, truncated: map[DocID]bool{}, read: map[DocID]bool{}}
}

// document adds r, doc's own resource, and returns its index.
func (ix *identifiers) document(doc DocID, r resource) int32 {
	i := int32(len(ix.resources))
	ix.resources = append(ix.resources, r)
	ix.own[doc] = i
	return i
}

// declare notes what the schema n, read at at in the resource indexed in,
// declares, and returns the index of the resource its content is in: its own
// where it writes an $id naming another URI (resources.enter).
func (ix *identifiers) declare(r resources, in int32, at Position, n *yaml.Node) int32 {
	held := ix.resources[in]
	res, rooted, known := r.enter(held, n)
	ix.truncated[at.Doc] = ix.truncated[at.Doc] || !known
	switch {
	case rooted:
		res.root = at
		ix.pending[res.uri] = append(ix.pending[res.uri], at)
		ix.resources = append(ix.resources, res)
		in = int32(len(ix.resources) - 1)
	case res != held:
		in = ix.own[at.Doc]
	}
	view := r.docs.list[at.Doc].view
	for _, keyword := range []string{"$anchor", "$dynamicAnchor"} {
		name, ok := scalarOf(view.ChildByToken(nodeview.Deref(n), keyword))
		if !ok || !isAnchorName(name) {
			continue
		}
		k := anchorKey{root: res.root, name: name}
		ix.anchors[k] = append(ix.anchors[k], at)
		if keyword == "$dynamicAnchor" {
			d := dynamicKey{doc: at.Doc, name: name}
			ix.dynamic[d] = append(ix.dynamic[d], at)
		}
	}
	return in
}

// commit makes the URIs declared since it last ran answer, and reports whether
// a pass asked for one of them, whose answer is then not the one the final index
// gives: a URI it was told none declares, or one a second schema now declares,
// which so names neither.
func (ix *identifiers) commit() bool {
	stale := false
	for uri, at := range ix.pending {
		ix.uris[uri] = append(ix.uris[uri], at...)
		stale = stale || ix.asked[uri]
	}
	clear(ix.pending)
	return stale
}

// lookupURI returns the root of the one resource uri identifies, and whether
// one does. Where none does, why is DuplicateID for two or more, and NoReason
// otherwise, uri then naming a document.
func (ix *identifiers) lookupURI(uri string) (Position, bool, Reason) {
	ix.asked[uri] = true
	found := ix.uris[uri]
	switch len(found) {
	case 0:
		return Position{}, false, NoReason
	case 1:
		return found[0], true, NoReason
	default:
		return Position{}, false, DuplicateID
	}
}

// lookupAnchor returns the schema the resource rooted at root declares name
// in, or why none answers: DuplicateAnchor for two or more, MergeBound where a
// merge past the bound may hide the one that would, and NoAnchor otherwise.
func (ix *identifiers) lookupAnchor(root Position, name string) (Position, Reason) {
	found := ix.anchors[anchorKey{root: root, name: name}]
	switch {
	case len(found) == 1:
		return found[0], NoReason
	case len(found) > 1:
		return Position{}, DuplicateAnchor
	case ix.truncated[root.Doc]:
		return Position{}, MergeBound
	default:
		return Position{}, NoAnchor
	}
}

// enter is the resource the content of the schema n, held in the resource in,
// is in: a new one, named by its $id, where it writes one naming another URI
// than in's; the document's own where its $id names its document; else in. An
// $id naming in's own URI starts none. rooted reports that n roots the
// resource returned, whose root the caller sets. known is false where n writes
// no $id and merges past the view's bound, which may hide one.
func (r resources) enter(in resource, n *yaml.Node) (res resource, rooted, known bool) {
	id, written := r.idOf(in.root.Doc, n)
	if !written {
		return in, false, r.docs.list[in.root.Doc].view.Expanded(nodeview.Deref(n))
	}
	uri, part, ok := r.identify(id, in.uri)
	switch {
	case !ok || uri == in.uri:
		return in, false, true
	case r.names(in.root.Doc, uri):
		return r.document(in.root.Doc), false, true
	default:
		detached := in.detached || absolute(part) || strings.HasPrefix(part, "/")
		return resource{uri: uri, id: true, detached: detached}, true, true
	}
}

// names reports whether uri, as resolveURI spells one, names document id: its
// name, or the name normalized where it is an absolute URI.
func (r resources) names(id DocID, uri string) bool {
	name := r.docs.list[id].name
	if uri == name {
		return true
	}
	u, err := url.Parse(name)
	return err == nil && len(u.Scheme) > 1 && uri == normalURI(u)
}

// identify is the URI an $id of id names, written where the base is base, and
// the part it is spelled by: its part before the '#', trimmed, resolved
// against base (resolveURI), or base for an $id naming no document. An $id
// with a fragment that is not empty names none, as JSON Schema 2020-12 §8.2.1
// admits none.
func (r resources) identify(id, base string) (uri, part string, ok bool) {
	part, frag, _ := strings.Cut(id, "#")
	if frag != "" {
		return "", "", false
	}
	if part = strings.TrimSpace(part); part == "" {
		return base, part, true
	}
	uri, ok = r.resolveURI(part, base)
	return uri, part, ok
}

// absolute reports whether uri is an absolute URI, which no base moves: one
// with a scheme of more than one letter, as a drive's is a path's.
func absolute(uri string) bool {
	u, err := url.Parse(uri)
	return err == nil && len(u.Scheme) > 1
}

// resolveURI is the URI ref names, written where the base is base: resolved by
// RFC 3986 §5.2 where base is an absolute URI, as an $id gives one, and placed
// as a $ref's document part is (docs.locate) where base is a document's
// location, such as a file's path. An absolute result is spelled as RFC 3986
// §6.2.2 normalizes one (normalURI), so two spellings of one identifier match.
func (r resources) resolveURI(ref, base string) (string, bool) {
	b, err := url.Parse(base)
	if err != nil || len(b.Scheme) < 2 {
		named, ok := r.docs.locate(ref, base)
		if u, err := url.Parse(named); ok && err == nil && len(u.Scheme) > 1 {
			return normalURI(u), true
		}
		return named, ok
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", false
	}
	return normalURI(b.ResolveReference(u)), true
}

// normalURI is the absolute URI u spelled as RFC 3986 §6.2.2 normalizes it:
// its scheme and host in lower case, its dot segments removed, a character it
// may not hold percent-encoded, and its percent-encodings normalized
// (normalEscapes).
func normalURI(u *url.URL) string {
	u.Host = strings.ToLower(u.Host)
	return normalEscapes(u.ResolveReference(&url.URL{}).String())
}

// normalEscapes is s with each percent-encoding of an unreserved character
// decoded and every other one's hex digits in upper case (RFC 3986 §6.2.2.1,
// §6.2.2.2). An encoding of any other character, such as "%2F", keeps its
// meaning, so it stays encoded, and a '%' opening none, which an opaque URI
// or a query may hold, stays as written.
func normalEscapes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c, ok := escaped(s, i)
		switch {
		case !ok:
			b.WriteByte(s[i])
			continue
		case unreserved(c):
			b.WriteByte(c)
		default:
			b.WriteString(strings.ToUpper(s[i : i+3]))
		}
		i += 2
	}
	return b.String()
}

// escaped is the byte the percent-encoding at s[i] spells, and false where
// none starts there.
func escaped(s string, i int) (byte, bool) {
	if s[i] != '%' || i+2 >= len(s) {
		return 0, false
	}
	hi, okHi := hexValue(s[i+1])
	lo, okLo := hexValue(s[i+2])
	return hi<<4 | lo, okHi && okLo
}

// hexValue is the value of the hex digit c, and false for a byte that is none.
func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

// unreserved reports whether c is an unreserved character of RFC 3986 §2.3.
func unreserved(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-._~", c) >= 0
}

// idOf is the $id n writes in document id, and whether it writes a non-empty
// string one: a null $id is none, as refOf reads a null $ref.
func (r resources) idOf(id DocID, n *yaml.Node) (string, bool) {
	return scalarOf(r.docs.list[id].view.ChildByToken(nodeview.Deref(n), "$id"))
}

// scalarOf is the string n holds, and whether it holds a non-empty one that
// is not null.
func scalarOf(n *yaml.Node) (string, bool) {
	v := nodeview.Deref(n)
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" || v.Value == "" {
		return "", false
	}
	return v.Value, true
}

// holding is the resource the position p, reached along path, is in: the one
// each schema on path enters in turn (enter), from its document's own.
// With schema, p is read as a schema whatever the grammar reads it as, as a
// $ref site is, so its own $id counts. known is false where a schema on path
// may hide an $id past the merge bound.
func (r resources) holding(p Position, path []*yaml.Node, schema bool) (resource, bool) {
	res, known := r.document(p.Doc), true
	s, tokens := r.docs.identityState(p.Doc), slices.Collect(p.Pointer.Tokens())
	for i, n := range path {
		if i > 0 {
			s = step(s, tokens[i-1])
		}
		if s != sSchema && s != sLoose && (i < len(path)-1 || !schema) {
			continue
		}
		next, rooted, ok := r.enter(res, n)
		known = known && ok
		if rooted {
			next.root = Position{Doc: p.Doc, Pointer: jsontext.Pointer(prefix(p.Pointer, i))}
		}
		res = next
	}
	return res, known
}
