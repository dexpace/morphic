// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

import (
	"cmp"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// resource is a schema resource (JSON Schema 2020-12 §8.2.1): the content of a
// document, rooted at its root and named by its name, or a schema whose $id
// names another URI than the resource holding it, rooted there and named by
// that URI. A reference written in one resolves against its name.
type resource struct {
	root DocumentPointer
	uri  string
	// namedByDollarID reports whether an $id names it, so that its name is an
	// identifier rather than a document's location.
	namedByDollarID bool
	// detached reports that an $id on the way to it from its document's own,
	// or the document's $self, names an absolute URI or path, so a read
	// against its base is judged by the URI it resolves to rather than by its
	// own relative spelling.
	detached bool
	// based reports that a document's $self names it, so a relative
	// reference written in it is read against that URI, as under an $id.
	based bool
}

// resources reads the schema resources of the documents a table reads: where
// each begins, the URI naming it, and the base a reference written in one is
// read against.
type resources struct {
	docs *docs
}

// document is the resource of document id's own content.
// A document whose $self names a URI is named and based by it
// (docs/compilers/openapi.md#self-uri).
func (r resources) document(id DocID) resource {
	document := r.docs.list[id]
	if document.self == "" {
		return resource{root: DocumentPointer{Doc: id}, uri: document.name}
	}
	return resource{root: DocumentPointer{Doc: id}, uri: document.self, based: true, detached: document.absolute}
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
	declared map[string][]DocumentPointer
	uris     map[string][]DocumentPointer
	counted  map[DocID]bool
	every    bool
	byDoc    map[DocID][]string
	scanned  DocID
	// asked is each URI a pass has asked for, whatever uris held, and readings
	// each set of documents counted a pass has begun with (reading).
	asked    map[string]bool
	readings map[string]bool
	anchors  map[anchorKey][]DocumentPointer
	// dynamic is every $dynamicAnchor by document and name, in any resource: a
	// reference expands only against the one declaration a document makes of
	// a name, so one in a resource of its own counts (docs/compilers/openapi.md#dynamic-ref).
	dynamic map[dynamicKey][]DocumentPointer
	// recursive is every `$recursiveAnchor` of true by document, where its
	// schema's dialect defines the keyword (DialectRule.Recursive).
	recursive map[DocID][]DocumentPointer
	// truncated holds each document with a schema merging through `<<` deeper
	// than the view expands, so an identifier past the bound may be missing.
	truncated map[DocID]bool
	read      map[DocID]bool
	// dialects is each schema resource root that writes a `$schema`, by where
	// it is: a schema no other holds (heldBySchema), or one whose identifier
	// roots a resource of its own (enterIn), and named each document holding
	// one.
	dialects map[DocumentPointer]DialectRoot
	named    map[DocID]bool
}

// anchorKey is a plain name and the root of the resource declaring it.
type anchorKey struct {
	root DocumentPointer
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
		declared: map[string][]DocumentPointer{}, uris: map[string][]DocumentPointer{}, counted: map[DocID]bool{Source: true},
		byDoc: map[DocID][]string{},
		asked: map[string]bool{}, readings: map[string]bool{}, anchors: map[anchorKey][]DocumentPointer{},
		dynamic: map[dynamicKey][]DocumentPointer{}, recursive: map[DocID][]DocumentPointer{}, truncated: map[DocID]bool{},
		read:     map[DocID]bool{},
		dialects: map[DocumentPointer]DialectRoot{}, named: map[DocID]bool{}}
}

// readsDialects reports whether a base in document id is read through the
// dialects its schemas name (resources.holding): where the document holds a
// dialect root, or is not read yet, as a chain followed into a document
// before any walk of it is, so it may hold any. One read holding none is read
// in its own dialect alone, which needs no `$schema` read on the way.
func (ix *identifiers) readsDialects(id DocID) bool {
	return ix.named[id] || !ix.read[id]
}

// document adds resourceValue, doc's own resource, and returns its index.
// Another document than the source declares the URI its $self names at its
// root, as an $id declares one; the source is named by its $self directly
// (names).
func (ix *identifiers) document(doc DocID, resourceValue resource) int32 {
	i := int32(len(ix.resources))
	ix.resources = append(ix.resources, resourceValue)
	ix.own[doc] = i
	if resourceValue.based && doc != Source {
		ix.declared[resourceValue.uri] = append(ix.declared[resourceValue.uri], resourceValue.root)
		ix.byDoc[doc] = append(ix.byDoc[doc], resourceValue.uri)
	}
	return i
}

// declare notes what the schema node, read at at in the resource indexed in and
// with its parent in the dialect enclosing, declares, the dialect its `$schema`
// names among it where node roots a resource (enterIn), and returns the index
// of the resource its content is in, its own where it writes an identifier
// naming another URI, and the dialect that content is read in.
func (ix *identifiers) declare(resourcesValue resources, in int32, at DocumentPointer, node *yaml.Node, enclosing string) (int32, string) {
	held := ix.resources[in]
	entering := resourcesValue.enterIn(held, at, node, enclosing, func() bool { return heldAt(resourcesValue.docs, at) })
	res := entering.res
	ix.truncated[at.Doc] = ix.truncated[at.Doc] || !entering.known
	switch {
	case entering.rooted:
		res.root = at
		ix.declared[res.uri] = append(ix.declared[res.uri], at)
		ix.byDoc[at.Doc] = append(ix.byDoc[at.Doc], res.uri)
		ix.resources = append(ix.resources, res)
		in = int32(len(ix.resources) - 1)
	case res != held:
		in = ix.own[at.Doc]
	}
	view := resourcesValue.docs.list[at.Doc].view
	if entering.names {
		ix.dialects[at] = DialectRoot{At: at, URI: entering.dialect, Node: nodeview.Deref(node)}
		ix.named[at.Doc] = true
	}
	rule := dialectRule(resourcesValue.docs, at.Doc, entering.dialect)
	for _, keyword := range []string{"$anchor", "$dynamicAnchor", rule.Keyword} {
		name, ok := scalarOf(view.ChildByToken(nodeview.Deref(node), keyword))
		switch {
		case keyword == rule.Keyword:
			name, ok = fragmentName(rule, name, ok && rule.Fragments && !resourcesValue.ignoresIdentifier(at.Doc, node, rule))
		case ok:
			ok = anchorNamed(rule, keyword, name)
		}
		if !ok {
			continue
		}
		anchorKeyValue := anchorKey{root: res.root, name: name}
		ix.anchors[anchorKeyValue] = append(ix.anchors[anchorKeyValue], at)
		if keyword == "$dynamicAnchor" {
			dynamicKeyValue := dynamicKey{doc: at.Doc, name: name}
			ix.dynamic[dynamicKeyValue] = append(ix.dynamic[dynamicKeyValue], at)
		}
	}
	if rule.Recursive && recursiveAnchored(view, node) {
		ix.recursive[at.Doc] = append(ix.recursive[at.Doc], at)
	}
	return in, entering.dialect
}

// admitted reports whether a declaration in document decl answers a reference
// to uri written in document from: only where from could read decl
// (docs/compilers/openapi.md#references). A document answers itself, so a
// compound one resolves within itself. One read over http(s) answers others
// only for a URI of its own origin, scheme, host and port, so it supplies no
// other host's content; and a document read over http(s) is answered by no
// file, nor by a source that is one, which it reaches by no route.
func admitted(docsValue *docs, decl, from DocID, uri string) bool {
	declarer, asker := docsValue.list[decl].name, docsValue.list[from].name
	switch {
	case decl == from:
		return true
	case !Reachable(asker, declarer):
		return false
	case !OverNetwork(declarer):
		return true
	}
	return sameOrigin(uri, declarer)
}

// sameOrigin reports whether the URIs a and b, each spelled as NormalURI
// spells one, share a scheme and an authority; a URI with none, such as a
// urn:, shares it with nothing.
func sameOrigin(a, b string) bool {
	u, errU := url.Parse(a)
	v, errV := url.Parse(b)
	return errU == nil && errV == nil && u.Host != "" && u.Scheme == v.Scheme && u.Host == v.Host
}

// recursiveAnchored reports whether the schema node writes `$recursiveAnchor`
// as the boolean true, which marks its resource for a `$recursiveRef`; false,
// the default, and any other value mark none.
func recursiveAnchored(view *nodeview.View, node *yaml.Node) bool {
	recursiveAnchorNode := nodeview.Deref(view.ChildByToken(nodeview.Deref(node), "$recursiveAnchor"))
	return recursiveAnchorNode != nil && recursiveAnchorNode.Kind == yaml.ScalarNode && recursiveAnchorNode.Tag == "!!bool" && strings.EqualFold(recursiveAnchorNode.Value, "true")
}

// commit makes the URIs the documents counted declare answer, and reports
// whether that changes the declarations of a URI a pass asked for, so its
// answer is not theirs: a document read since declares it, or one a pass no
// longer reaches withdraws it (ReferencedDocument.Unreached). Where what is
// reached turns on what is declared, no pass may agree with the one before it,
// so once a change would return to declarations a pass read against, every
// document read counts, as one read declares what it declares however it was
// reached, and that only grows.
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
func (ix *identifiers) answering() map[string][]DocumentPointer {
	uris := map[string][]DocumentPointer{}
	for uri := range ix.declared {
		if at := ix.answer(uri); len(at) > 0 {
			uris[uri] = at
		}
	}
	return uris
}

// answer returns the declarations of uri commit counts.
func (ix *identifiers) answer(uri string) []DocumentPointer {
	var at []DocumentPointer
	for _, documentPointer := range ix.declared[uri] {
		if ix.every || ix.counted[documentPointer.Doc] {
			at = append(at, documentPointer)
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
	for _, docID := range reached {
		ix.counted[docID] = true
		for _, uri := range ix.byDoc[docID] {
			affected[uri] = true
		}
	}
	// Each URI here is declared by a document now counted, so none answers
	// with no declaration.
	next := map[string][]DocumentPointer{}
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
func (ix *identifiers) changes(uris map[string][]DocumentPointer) bool {
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

// lookupURI returns the root of the one resource uri identifies to an asker,
// and whether one does, counting only the declarations of documents answers
// says answer it. Where none does, why is DuplicateID for two or more, and
// NoReason otherwise, uri then naming a document; waitOn is the documents read
// and not yet counted that declare it to the asker, where mayWait lets the
// asker wait for one to be reached rather than read uri as a location. Which
// URIs a pass asked for is the caller's to note, once its answer is kept
// (keep).
func (ix *identifiers) lookupURI(uri string, answers func(decl DocID) bool, mayWait bool) (DocumentPointer, bool, Reason, []DocID) {
	var found []DocumentPointer
	for _, documentPointer := range ix.uris[uri] {
		if answers(documentPointer.Doc) {
			found = append(found, documentPointer)
		}
	}
	switch {
	case len(found) == 1:
		return found[0], true, NoReason, nil
	case len(found) > 1:
		return DocumentPointer{}, false, DuplicateID, nil
	case !mayWait || ix.every:
		return DocumentPointer{}, false, NoReason, nil
	}
	return DocumentPointer{}, false, NoReason, ix.pending(uri, answers)
}

// pending returns the documents read and not counted that declare uri to an
// asker answers admits: each declaration answers once its document is reached.
func (ix *identifiers) pending(uri string, answers func(decl DocID) bool) []DocID {
	var docs []DocID
	for _, documentPointer := range ix.declared[uri] {
		if !ix.counted[documentPointer.Doc] && answers(documentPointer.Doc) {
			docs = append(docs, documentPointer.Doc)
		}
	}
	return docs
}

// keep notes that a pass asked for uri and kept the answer it got, so a later
// change of its declarations changes an answer the pass gave (commit, grow).
func (ix *identifiers) keep(uri string) { ix.asked[uri] = true }

// lookupAnchor returns the schema the resource rooted at root declares name
// in, or why none answers: DuplicateAnchor for two or more, MergeBound where a
// merge past the bound may hide the one that would, and NoAnchor otherwise.
func (ix *identifiers) lookupAnchor(root DocumentPointer, name string) (DocumentPointer, Reason) {
	found := ix.anchors[anchorKey{root: root, name: name}]
	switch {
	case len(found) == 1:
		return found[0], NoReason
	case len(found) > 1:
		return DocumentPointer{}, DuplicateAnchor
	case ix.truncated[root.Doc]:
		return DocumentPointer{}, MergeBound
	default:
		return DocumentPointer{}, NoAnchor
	}
}

// enter is the resource the content of the schema node, held in the resource in
// and read as rule says, is in: a new one, named by its identifier, where it
// writes one naming another URI than in's; the document's own where its
// identifier names its document; else in. An identifier naming in's own URI
// starts none, and so does one beside a `$ref` rule ignores
// (ignoresIdentifier). rooted reports that node roots the resource returned,
// whose root the caller sets. known is false where node writes no identifier
// and merges past the view's bound, which may hide one.
func (r resources) enter(in resource, node *yaml.Node, rule DialectRule) (res resource, rooted, known bool) {
	if r.ignoresIdentifier(in.root.Doc, node, rule) {
		return in, false, true
	}
	id, written := r.idOf(in.root.Doc, node, rule.Keyword)
	if !written {
		return in, false, r.docs.list[in.root.Doc].view.Expanded(nodeview.Deref(node))
	}
	uri, part, ok := r.identify(id, in.uri)
	switch {
	case !ok || uri == in.uri:
		return in, false, true
	case r.names(in.root.Doc, uri):
		return r.document(in.root.Doc), false, true
	default:
		detached := in.detached || fragment.Absolute(part) || strings.HasPrefix(part, "/")
		return resource{uri: uri, namedByDollarID: true, detached: detached}, true, true
	}
}

// names reports whether uri, as resolveURI spells one, names document id: its
// name, the URI its $self names, or the name normalized where it is an
// absolute URI.
func (r resources) names(id DocID, uri string) bool {
	name := r.docs.list[id].name
	if uri == name || uri == r.docs.list[id].self {
		return true
	}
	u, err := url.Parse(name)
	return err == nil && len(u.Scheme) > 1 && uri == NormalURI(u)
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

// resolveURI is the URI ref names, written where the base is base: resolved by
// RFC 3986 §5.2 where base is an absolute URI, as an $id gives one, and placed
// as a $ref's document part is (docs.locate) where base is a document's
// location, such as a file's path. An absolute result is spelled as RFC 3986
// §6.2.2 normalizes one (NormalURI), so two spellings of one identifier match.
func (r resources) resolveURI(ref, base string) (string, bool) {
	b, err := url.Parse(base)
	if err != nil || len(b.Scheme) < 2 {
		named, ok := r.docs.locate(ref, base)
		if u, err := url.Parse(named); ok && err == nil && len(u.Scheme) > 1 {
			return NormalURI(u), true
		}
		return named, ok
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", false
	}
	return NormalURI(b.ResolveReference(u)), true
}

// NormalURI is the absolute URI u spelled as RFC 3986 §6.2.2 normalizes it,
// and an http or https one as §6.2.3 does too: its scheme and host in lower
// case; each escape of an unreserved character decoded and every other one's
// hex in upper case (normalEscapes), before its dot segments are removed, so
// an escaped dot segment is removed and a second pass changes nothing; and
// for http and https an empty path made "/", and an empty or default port
// dropped. An empty query's "?" and a host's final dot stay.
func NormalURI(u *url.URL) string {
	n := *u
	n.Host = strings.ToLower(n.Host)
	if n.Opaque != "" {
		return normalEscapes(n.String())
	}
	path := removeDots(normalEscapes(n.EscapedPath()))
	if n.Scheme == "http" || n.Scheme == "https" {
		n.Host = strings.TrimSuffix(strings.TrimSuffix(n.Host, ":"), defaultPort(n.Scheme))
		path = cmp.Or(path, "/")
	}
	hasQuery, hasFragment := n.ForceQuery || n.RawQuery != "", n.Fragment != ""
	rawQuery, rawFragment := n.RawQuery, n.EscapedFragment()
	// A path of "/" has String write the authority's "//" wherever the URL
	// wrote one, an empty host's included, which is then trimmed to the "/".
	n.Path, n.RawPath, n.RawQuery, n.ForceQuery, n.Fragment, n.RawFragment = "/", "", "", false, "", ""
	var b strings.Builder
	b.WriteString(strings.TrimSuffix(n.String(), "/") + path)
	if hasQuery {
		b.WriteString("?" + normalEscapes(rawQuery))
	}
	if hasFragment {
		b.WriteString("#" + normalEscapes(rawFragment))
	}
	return b.String()
}

// defaultPort is the port RFC 3986 §6.2.3 has an http or https URL default
// to, scheme naming which, as a host's suffix.
func defaultPort(scheme string) string {
	if scheme == "https" {
		return ":443"
	}
	return ":80"
}

// removeDots is the path p, escaped and holding no escape of a dot, with its
// dot segments removed as RFC 3986 §5.2.4 removes them: "." is no segment,
// ".." takes the one before it away, and either ending p leaves p ending in
// "/".
func removeDots(p string) string {
	if !strings.Contains(p, ".") {
		return p
	}
	segments := strings.Split(p, "/")
	out := make([]string, 0, len(segments))
	for i, s := range segments {
		switch {
		case s == "..":
			out = out[:max(len(out)-1, min(len(out), 1))]
		case s != ".":
			out = append(out, s)
			continue
		}
		if i == len(segments)-1 {
			out = append(out, "")
		}
	}
	return strings.Join(out, "/")
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

// idOf is the identifier node writes in document id under keyword, and whether
// it writes a non-empty string one: a null one is none, as refOf reads a null
// $ref.
func (r resources) idOf(id DocID, node *yaml.Node, keyword string) (string, bool) {
	return scalarOf(r.docs.list[id].view.ChildByToken(nodeview.Deref(node), keyword))
}

// ignoresIdentifier reports whether the schema node of document id, read as rule
// says, writes a $ref whose siblings rule ignores (DialectRule.IgnoresRefFields),
// so that its identifier names no resource, gives no base and declares no name,
// as the JSON Schema drafts before 2019-09 have it.
func (r resources) ignoresIdentifier(id DocID, node *yaml.Node, rule DialectRule) bool {
	if !rule.IgnoresRefFields {
		return false
	}
	_, ref := r.docs.refOf(id, node)
	return ref
}

// fragmentName returns the plain name the identifier id declares in a schema
// read as rule says, where it names a fragment alone, "#name", as the drafts
// before 2019-09 have one: of the plain-name grammar where rule gives it
// (DialectRule.PlainFragments), as drafts 6 and 7 do, else any no JSON pointer
// spells, as in draft 4, read as land reads a $ref's fragment. It is false for
// any other, a fragment holding a second '#' among them, as a $ref writing one
// names nothing, or where written is false.
func fragmentName(rule DialectRule, id string, written bool) (string, bool) {
	raw, ok := strings.CutPrefix(id, "#")
	switch {
	case !written || !ok || raw == "" || strings.Contains(raw, "#"):
		return "", false
	case rule.PlainFragments:
		return raw, specrules.ValidFormat(specrules.PlainName, raw)
	}
	name := string(fragment.Pointer(id))
	return name, name != "" && !strings.HasPrefix(name, "/") && utf8.ValidString(name)
}

// scalarOf is the string node holds, and whether it holds a non-empty one that
// is not null.
func scalarOf(node *yaml.Node) (string, bool) {
	dereferenced := nodeview.Deref(node)
	if dereferenced == nil || dereferenced.Kind != yaml.ScalarNode || dereferenced.Tag == "!!null" || dereferenced.Value == "" {
		return "", false
	}
	return dereferenced.Value, true
}

// holding is the resource the position documentPointer, reached along path, is
// in: the one each schema on path enters in turn (enter), from its document's
// own. With schema, documentPointer is read as a schema whatever the grammar
// reads it as, as a $ref site is, so its own $id counts. Where named, a schema
// of the documents names its dialect, and each schema on path is read in the
// one the schema holding it enters (enterIn); else every schema is read in its
// document's. known is false where a schema on path may hide an identifier past
// the merge bound.
func (r resources) holding(documentPointer DocumentPointer, path []*yaml.Node, schema, named bool) (resource, bool) {
	res, known := r.document(documentPointer.Doc), true
	state, tokens := r.docs.identityState(documentPointer.Doc), slices.Collect(documentPointer.Pointer.Tokens())
	dialect, held := defaultDialect(r.docs, documentPointer.Doc), false
	rule := dialectRule(r.docs, documentPointer.Doc, dialect)
	for i, node := range path {
		if i > 0 {
			held = held || state == sSchema || state == sLoose
			state = stepIn(r.docs, documentPointer.Doc, path[i-1], state, tokens[i-1], dialect)
		}
		if state != sSchema && state != sLoose && (i < len(path)-1 || !schema) {
			continue
		}
		enteringValue := entering{dialect: dialect}
		if named {
			enteringValue = r.enterIn(res, DocumentPointer{Doc: documentPointer.Doc}, node, dialect, func() bool { return held })
		} else {
			enteringValue.res, enteringValue.rooted, enteringValue.known = r.enter(res, node, rule)
		}
		known = known && enteringValue.known
		if enteringValue.rooted {
			enteringValue.res.root = DocumentPointer{Doc: documentPointer.Doc, Pointer: jsontext.Pointer(prefix(documentPointer.Pointer, i))}
		}
		res, dialect = enteringValue.res, enteringValue.dialect
	}
	return res, known
}
