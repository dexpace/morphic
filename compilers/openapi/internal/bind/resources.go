package bind

import (
	"encoding/json/jsontext"
	"net/url"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

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
}

// documentResource is the resource of document id's own content.
func (d *docs) documentResource(id DocID) resource {
	return resource{root: Position{Doc: id}, uri: d.list[id].name}
}

// identifiers is every identifier the source's schemas declare: each URI an
// $id names, and each plain name an $anchor or a $dynamicAnchor declares, by
// the root of the resource it is declared in. Only the schemas the OpenAPI
// grammar places are read, as JSON Schema 2020-12 §9.4.2 recognizes no
// identifier anywhere else; another document's are not read until its schemas
// are lowered (GitHub #74).
type identifiers struct {
	// resources is every resource the source's walk reads in, the document's
	// own first, which a walk's frame names by index.
	resources []resource
	uris      map[string][]Position
	anchors   map[anchorKey][]Position
	// truncated reports that a schema merges through `<<` deeper than the view
	// expands, so an identifier past the bound may be missing.
	truncated bool
}

// anchorKey is a plain name and the root of the resource declaring it.
type anchorKey struct {
	root Position
	name string
}

// newIdentifiers returns an index holding no identifier, whose first resource
// is source, the source document's own.
func newIdentifiers(source resource) *identifiers {
	return &identifiers{resources: []resource{source}, uris: map[string][]Position{},
		anchors: map[anchorKey][]Position{}}
}

// declare notes what the schema n, read at at in the resource indexed in,
// declares, and returns the index of the resource its content is in: its own
// where it writes an $id naming another URI (docs.enter).
func (ix *identifiers) declare(d *docs, in int32, at Position, n *yaml.Node) int32 {
	held := ix.resources[in]
	res, rooted, known := d.enter(held, n)
	ix.truncated = ix.truncated || !known
	switch {
	case rooted:
		res.root = at
		ix.uris[res.uri] = append(ix.uris[res.uri], at)
		ix.resources = append(ix.resources, res)
		in = int32(len(ix.resources) - 1)
	case res != held:
		in = 0 // the document's own
	}
	view := d.list[at.Doc].view
	for _, keyword := range []string{"$anchor", "$dynamicAnchor"} {
		if name, ok := scalarOf(view.ChildByToken(nodeview.Deref(n), keyword)); ok && isAnchorName(name) {
			k := anchorKey{root: res.root, name: name}
			ix.anchors[k] = append(ix.anchors[k], at)
		}
	}
	return in
}

// lookupURI returns the root of the one resource uri identifies, or why none
// answers: two or more schemas declare it, or none does, or the merge bound
// may hide the one that would.
func (ix *identifiers) lookupURI(uri string) (Position, Reason) {
	return ix.one(ix.uris[uri], DuplicateID, UnknownID)
}

// lookupAnchor returns the schema the resource rooted at root declares name
// in, or why none answers, as lookupURI.
func (ix *identifiers) lookupAnchor(root Position, name string) (Position, Reason) {
	return ix.one(ix.anchors[anchorKey{root: root, name: name}], DuplicateAnchor, NoAnchor)
}

// one is the one position of found, else twice where it holds several, or
// none where it holds none and no merge past the bound may hide one.
func (ix *identifiers) one(found []Position, twice, none Reason) (Position, Reason) {
	switch {
	case len(found) == 1:
		return found[0], NoReason
	case len(found) > 1:
		return Position{}, twice
	case ix.truncated:
		return Position{}, MergeBound
	default:
		return Position{}, none
	}
}

// enter is the resource the content of the schema n, held in the resource in,
// is in: a new one, named by its $id, where it writes one naming another URI
// than in's; the document's own where its $id names its document; else in. An
// $id naming in's own URI starts none. rooted reports that n roots the
// resource returned, whose root the caller sets. known is false where n writes
// no $id and merges past the view's bound, which may hide one.
func (d *docs) enter(in resource, n *yaml.Node) (res resource, rooted, known bool) {
	id, written := d.idOf(in.root.Doc, n)
	if !written {
		return in, false, d.list[in.root.Doc].view.Expanded(nodeview.Deref(n))
	}
	uri, ok := d.identify(id, in.uri)
	switch {
	case !ok || uri == in.uri:
		return in, false, true
	case d.names(in.root.Doc, uri):
		return d.documentResource(in.root.Doc), false, true
	default:
		return resource{uri: uri, id: true}, true, true
	}
}

// names reports whether uri, as resolveURI spells one, names document id: its
// name, or the name normalized where it is an absolute URI.
func (d *docs) names(id DocID, uri string) bool {
	name := d.list[id].name
	if uri == name {
		return true
	}
	u, err := url.Parse(name)
	return err == nil && len(u.Scheme) > 1 && uri == normalURI(u)
}

// identify is the URI an $id of id names, written where the base is base: its
// part before the '#', resolved against base (resolveURI), or base for an $id
// naming no document. An $id with a fragment that is not empty names none, as
// JSON Schema 2020-12 §8.2.1 admits none.
func (d *docs) identify(id, base string) (string, bool) {
	uri, frag, _ := strings.Cut(id, "#")
	if frag != "" {
		return "", false
	}
	if uri = strings.TrimSpace(uri); uri == "" {
		return base, true
	}
	return d.resolveURI(uri, base)
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
func (d *docs) resolveURI(ref, base string) (string, bool) {
	b, err := url.Parse(base)
	if err != nil || len(b.Scheme) < 2 {
		named, ok := d.locate(ref, base)
		if u, err := url.Parse(named); ok && err == nil && len(u.Scheme) > 1 {
			return normalURI(u), true
		}
		return named, ok
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", false
	}
	return normalURI(b.ResolveReference(r)), true
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
func (d *docs) idOf(id DocID, n *yaml.Node) (string, bool) {
	return scalarOf(d.list[id].view.ChildByToken(nodeview.Deref(n), "$id"))
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

// resourceOf is the resource the position p, reached along path, is in: the
// one each schema on path enters in turn (enter), from its document's own.
// With schema, p is read as a schema whatever the grammar reads it as, as a
// $ref site is, so its own $id counts. known is false where a schema on path
// may hide an $id past the merge bound.
func (d *docs) resourceOf(p Position, path []*yaml.Node, schema bool) (resource, bool) {
	res, known := d.documentResource(p.Doc), true
	s, tokens := d.rootState(p.Doc), slices.Collect(p.Pointer.Tokens())
	for i, n := range path {
		if i > 0 {
			s = step(s, tokens[i-1])
		}
		if s != sSchema && (i < len(path)-1 || !schema) {
			continue
		}
		next, rooted, ok := d.enter(res, n)
		known = known && ok
		if rooted {
			next.root = Position{Doc: p.Doc, Pointer: jsontext.Pointer(prefix(p.Pointer, i))}
		}
		res = next
	}
	return res, known
}
