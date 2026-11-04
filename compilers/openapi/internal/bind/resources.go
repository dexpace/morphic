// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
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
// that document is read. A URI names no document, so a pass reads one against
// what the documents the pass before it reached declare (reread).
type identifiers struct {
	// resources is every resource read, each document's own among them, which
	// a walk's frame names by index; own is the index of each document's.
	resources []resource
	own       map[DocID]int32
	// declared is each URI an $id names, by the schemas declaring it, in every
	// document read, and uris those of them the documents counted declare,
	// which alone answer, or all of them once every is set (commit). byDoc is
	// the URIs each document declares (grow), and scanned how many documents
	// identifyAll has read.
	declared map[string][]Position
	uris     map[string][]Position
	counted  map[DocID]bool
	every    bool
	byDoc    map[DocID][]string
	scanned  DocID
	// asked is each URI a pass has asked for, whatever uris held, and readings
	// each set of documents counted a pass has begun with (reading).
	asked    map[string]bool
	readings map[string]bool
	anchors  map[anchorKey][]Position
	// dynamic is every $dynamicAnchor by document and name, in any resource: a
	// reference expands only against the one declaration a document makes of
	// a name, so one in a resource of its own counts (docs/compilers/openapi.md#dynamic-ref).
	dynamic map[dynamicKey][]Position
	// truncated holds each document with a schema merging through `<<` deeper
	// than the view expands, so an identifier past the bound may be missing.
	truncated map[DocID]bool
	read      map[DocID]bool
	// dialects is each schema resource root that writes a `$schema`, by where
	// it is: a schema no other holds (heldBySchema), or one whose identifier
	// roots a resource of its own (enterIn).
	dialects map[Position]DialectRoot
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
	return &identifiers{resources: []resource{source}, own: map[DocID]int32{Source: 0},
		declared: map[string][]Position{}, uris: map[string][]Position{}, counted: map[DocID]bool{Source: true},
		byDoc: map[DocID][]string{},
		asked: map[string]bool{}, readings: map[string]bool{}, anchors: map[anchorKey][]Position{},
		dynamic: map[dynamicKey][]Position{}, truncated: map[DocID]bool{}, read: map[DocID]bool{},
		dialects: map[Position]DialectRoot{}}
}

// document adds r, doc's own resource, and returns its index.
func (ix *identifiers) document(doc DocID, r resource) int32 {
	i := int32(len(ix.resources))
	ix.resources = append(ix.resources, r)
	ix.own[doc] = i
	return i
}

// declare notes what the schema n, read at at in the resource indexed in and
// with its parent in the dialect enclosing, declares, the dialect its
// `$schema` names among it where n roots a resource (enterIn), and returns the
// index of the resource its content is in, its own where it writes an
// identifier naming another URI, and the dialect that content is read in.
func (ix *identifiers) declare(r resources, in int32, at Position, n *yaml.Node, enclosing string) (int32, string) {
	held := ix.resources[in]
	e := r.enterIn(held, at, n, enclosing, func() bool { return heldAt(r.docs, at) })
	res := e.res
	ix.truncated[at.Doc] = ix.truncated[at.Doc] || !e.known
	switch {
	case e.rooted:
		res.root = at
		ix.declared[res.uri] = append(ix.declared[res.uri], at)
		ix.byDoc[at.Doc] = append(ix.byDoc[at.Doc], res.uri)
		ix.resources = append(ix.resources, res)
		in = int32(len(ix.resources) - 1)
	case res != held:
		in = ix.own[at.Doc]
	}
	view := r.docs.list[at.Doc].view
	if e.names {
		ix.dialects[at] = DialectRoot{At: at, URI: e.dialect, Node: nodeview.Deref(n)}
	}
	rule := dialectRule(r.docs, at.Doc, e.dialect)
	for _, keyword := range []string{"$anchor", "$dynamicAnchor", rule.Keyword} {
		name, ok := scalarOf(view.ChildByToken(nodeview.Deref(n), keyword))
		if keyword == rule.Keyword {
			name, ok = fragmentOnly(name, ok && rule.Fragments)
		}
		if !ok || !anchorNamed(rule, name) {
			continue
		}
		k := anchorKey{root: res.root, name: name}
		ix.anchors[k] = append(ix.anchors[k], at)
		if keyword == "$dynamicAnchor" {
			d := dynamicKey{doc: at.Doc, name: name}
			ix.dynamic[d] = append(ix.dynamic[d], at)
		}
	}
	return in, e.dialect
}

// commit makes the URIs the documents counted declare answer, and reports
// whether that changes the declarations of a URI a pass asked for, so its
// answer is not theirs: a document read since declares it, or one a pass no
// longer reaches withdraws it (Document.Unreached). Where what is reached turns
// on what is declared, no pass may agree with the one before it, so once a
// change would return to declarations a pass read against, every document read
// counts, as one read declares what it declares however it was reached, and
// that only grows.
func (ix *identifiers) commit(counted map[DocID]bool) bool {
	ix.counted = maps.Clone(counted)
	uris := ix.answering()
	if !ix.every && ix.changes(uris) && ix.readings[ix.reading()] {
		ix.every = true
		uris = ix.answering()
	}
	changed := ix.changes(uris)
	ix.uris = uris
	return changed
}

// begin makes the declarations commit counts answer for a pass, and notes that
// a pass read against them.
func (ix *identifiers) begin() {
	ix.uris = ix.answering()
	ix.readings[ix.reading()] = true
}

// answering returns the declarations commit counts, by URI.
func (ix *identifiers) answering() map[string][]Position {
	uris := map[string][]Position{}
	for uri := range ix.declared {
		if at := ix.answer(uri); len(at) > 0 {
			uris[uri] = at
		}
	}
	return uris
}

// answer returns the declarations of uri commit counts.
func (ix *identifiers) answer(uri string) []Position {
	var at []Position
	for _, p := range ix.declared[uri] {
		if ix.every || ix.counted[p.Doc] {
			at = append(at, p)
		}
	}
	return at
}

// grow counts the documents reached as well, for a pass that has only added
// to what it reached since uris was last answered, and reports whether that
// changes the declarations of a URI a pass asked for, which it then leaves
// for commit to judge. A document is reached when it is opened and read by the
// drain that counts it, so only the URIs those documents declare are answered
// again, and a pass that goes on past what it held pays for what it adds.
func (ix *identifiers) grow(reached []DocID) bool {
	affected := map[string]bool{}
	for _, d := range reached {
		ix.counted[d] = true
		for _, uri := range ix.byDoc[d] {
			affected[uri] = true
		}
	}
	// Each URI here is declared by a document now counted, so none answers
	// with no declaration.
	next := map[string][]Position{}
	for uri := range affected {
		next[uri] = ix.answer(uri)
		if ix.asked[uri] && !slices.Equal(next[uri], ix.uris[uri]) {
			return true
		}
	}
	maps.Copy(ix.uris, next)
	return false
}

// changes reports whether uris changes the declarations of a URI a pass asked
// for from those it read.
func (ix *identifiers) changes(uris map[string][]Position) bool {
	for uri := range ix.asked {
		if !slices.Equal(uris[uri], ix.uris[uri]) {
			return true
		}
	}
	return false
}

// reading names the declarations answering counts, before every document read
// counts, by the documents counted: each is read whole before a drain counts it
// (settled), and what one read and not counted declares answers nothing.
func (ix *identifiers) reading() string {
	return fmt.Sprint(slices.Sorted(maps.Keys(ix.counted)))
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
func (r resources) enter(in resource, n *yaml.Node, keyword string) (res resource, rooted, known bool) {
	id, written := r.idOf(in.root.Doc, n, keyword)
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
		detached := in.detached || fragment.Absolute(part) || strings.HasPrefix(part, "/")
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

// IdentifierURI returns the URI the identifier id names, written where the base
// is base, an absolute URI such as a fetched document's, as the binder reads one
// (resources.identify): its part before the '#', trimmed, resolved by RFC 3986.
// local reports that a reference under it may name a location on disk: a
// file: URI, or one whose scheme is a single letter, as a Windows drive's is,
// which resolveURI does not resolve as a URL. It is false where id names none,
// or base is no absolute URI.
func IdentifierURI(id, base string) (uri string, local, ok bool) {
	if b, err := url.Parse(base); err != nil || len(b.Scheme) < 2 {
		return "", false, false
	}
	if uri, _, ok = (resources{}).identify(id, base); !ok {
		return "", false, false
	}
	u, err := url.Parse(uri)
	return uri, err != nil || len(u.Scheme) < 2 || strings.EqualFold(u.Scheme, "file"), true
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
func (r resources) idOf(id DocID, n *yaml.Node, keyword string) (string, bool) {
	return scalarOf(r.docs.list[id].view.ChildByToken(nodeview.Deref(n), keyword))
}

// fragmentOnly returns the plain name id names where it names a fragment
// alone, "#name", as an identifier the drafts before 2019-09 declare an anchor
// by, and false for any other, or where written is false.
func fragmentOnly(id string, written bool) (string, bool) {
	name, ok := strings.CutPrefix(id, "#")
	return name, written && ok && name != ""
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
// each schema on path enters in turn (enter), from its document's own. With
// schema, p is read as a schema whatever the grammar reads it as, as a $ref
// site is, so its own $id counts. Where named, a schema of the documents names
// its dialect, and each schema on path is read in the one the schema holding it
// enters (enterIn); else every schema is read in its document's. known is false
// where a schema on path may hide an identifier past the merge bound.
func (r resources) holding(p Position, path []*yaml.Node, schema, named bool) (resource, bool) {
	res, known := r.document(p.Doc), true
	s, tokens := r.docs.identityState(p.Doc), slices.Collect(p.Pointer.Tokens())
	dialect, held := defaultDialect(r.docs, p.Doc), false
	keyword := dialectRule(r.docs, p.Doc, dialect).Keyword
	for i, n := range path {
		if i > 0 {
			held = held || s == sSchema || s == sLoose
			s = stepIn(r.docs, p.Doc, path[i-1], s, tokens[i-1], dialect)
		}
		if s != sSchema && s != sLoose && (i < len(path)-1 || !schema) {
			continue
		}
		e := entering{dialect: dialect}
		if named {
			e = r.enterIn(res, Position{Doc: p.Doc}, n, dialect, func() bool { return held })
		} else {
			e.res, e.rooted, e.known = r.enter(res, n, keyword)
		}
		known = known && e.known
		if e.rooted {
			e.res.root = Position{Doc: p.Doc, Pointer: jsontext.Pointer(prefix(p.Pointer, i))}
		}
		res, dialect = e.res, e.dialect
	}
	return res, known
}
