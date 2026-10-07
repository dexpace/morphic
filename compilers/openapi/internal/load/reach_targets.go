package load

import (
	"encoding/json/jsontext"
	"net/url"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"
)

// reachBase is the base a URI is resolved against to read what no base can
// change in it. It is deep enough that '..' never reaches the root, and its
// host is reserved, so it never names a real document.
const reachBase = "http://reach.invalid/a/b/c/d"

// refClass is everything the targets of a reference depend on: its spelling
// (interned as id), whether a /$defs/ pointer may have moved the document it
// resolves against, and, for a spelling that can name an $anchor or $id, the
// schema document it sits in. References of one class have the same targets,
// so the walk treats them as one vertex; a cycle among classes is a cycle among
// references.
//
// A held "#/$defs/..." reference is a class of its own, named by held, the
// node that is it: what it lands on is the definition its schemas name.
type refClass struct {
	id      int
	drifted bool
	scope   *yaml.Node
	held    *yaml.Node
}

// isDefsRef reports whether the resolver reads raw as a /$defs/ pointer. The
// library decides this on the decoded pointer between the first '#' and the
// next, so a percent-encoded '$' or a second '#' does not change it.
func isDefsRef(raw string) bool {
	if !strings.Contains(raw, "#") {
		return false
	}
	return strings.HasPrefix(string(references.Reference(raw).GetJSONPointer()), "/$defs/")
}

// namesRegistryEntry reports whether raw can resolve through the $anchor and
// $id registries, whose entries are per schema document: it carries a URI part
// or an anchor-shaped fragment.
func namesRegistryEntry(raw string) bool {
	return oas3.ExtractAnchor(raw) != "" || references.Reference(raw).GetURI() != ""
}

// lookups is every set a reference of class c can land in. A held reference
// lands on its definition alone. For the rest, the resolver asks the
// registries first, with the fragment as written, and falls back to a pointer
// read; a reference can be tried both ways, so both are kept. A pointer the
// resolver may read against a document other than the root, because it spells
// /$defs/ or the reference drifted, can start at any node; any other starts at
// the root. Drift and the search each ask once per class.
func (r *reach) lookups(c refClass) []*posSet {
	r.budget.spend(1)
	if c.held != nil {
		return []*posSet{r.held[c.held]}
	}
	raw := r.raws[c.id]
	ref := references.Reference(raw)
	var out []*posSet
	if anchor := oas3.ExtractAnchor(raw); anchor != "" {
		out = appendSet(out, r.decls.anchors[declKey{c.scope, anchor}])
	}
	if ref.GetURI() != "" {
		return r.idLandings(out, c.scope, ref)
	}
	if !ref.HasJSONPointer() {
		return out
	}
	switch pointer := string(ref.GetJSONPointer()); {
	case pointer == "" || pointer == "/":
		if c.drifted {
			out = appendSet(out, r.documents())
		}
		return out
	case !strings.HasPrefix(pointer, "/"):
		return out
	case c.drifted || strings.HasPrefix(pointer, "/$defs/"):
		return appendSet(out, r.tree.navigate(r.tree.anywhere, pointerTokens(pointer)))
	default:
		if t := r.tree.walk(r.tree.root, pointerTokens(pointer)); t != nil {
			out = append(out, &posSet{members: []*yaml.Node{t}})
		}
		return out
	}
}

// appendSet adds s to sets unless the lookup found no set.
func appendSet(sets []*posSet, s *posSet) []*posSet {
	if s == nil {
		return sets
	}
	return append(sets, s)
}

// documents is where an empty fragment can land: the document it is resolved
// against. That is the root, never a schema, unless a /$defs/ pointer moved it,
// and then it is the schema a chain arrived from or an ancestor the resolver
// searched for the definition. Only a drifted reference asks, and every one
// shares the set.
func (r *reach) documents() *posSet {
	if r.docs != nil {
		return r.docs
	}
	r.docs = &posSet{}
	r.budget.spend(len(r.schemas))
	for _, n := range r.schemas {
		if r.tree.refValue(n) != "" || r.searched[n] {
			r.docs.members = append(r.docs.members, n)
		}
	}
	return r.docs
}

// idLandings adds where a URI part can land within this document: a schema in
// the reference's scope whose $id could resolve to the same URI, navigated by
// the fragment when it is a pointer.
func (r *reach) idLandings(out []*posSet, scope *yaml.Node, ref references.Reference) []*posSet {
	var tokens []string
	switch pointer := string(ref.GetJSONPointer()); {
	case pointer == "" || pointer == "/":
	case strings.HasPrefix(pointer, "/"):
		tokens = pointerTokens(pointer)
	default:
		return out // the anchor half is already among them
	}
	for _, ids := range r.decls.identified(scope, keyOfURI(ref.GetURI())) {
		out = appendSet(out, r.tree.navigate(ids, tokens))
	}
	return out
}

// decls indexes the mappings that declare $anchor or $id, so a lookup reads
// the few it can name instead of scanning every declaration in its document.
// Each entry is a set every reference making that lookup shares.
type decls struct {
	anchors      map[declKey]*posSet
	idsBySegment map[declKey]*posSet
	// anyIDs and allIDs are per scope: the $ids with no path of their own, which
	// match every reference, and every $id, which a path-less reference matches.
	anyIDs map[*yaml.Node]*posSet
	allIDs map[*yaml.Node]*posSet
}

// declKey names one lookup within a schema document: a $anchor value, or the
// last segment of an $id.
type declKey struct {
	scope *yaml.Node
	name  string
}

func newDecls() decls {
	return decls{
		anchors: map[declKey]*posSet{}, idsBySegment: map[declKey]*posSet{},
		anyIDs: map[*yaml.Node]*posSet{}, allIDs: map[*yaml.Node]*posSet{},
	}
}

func (d decls) addAnchor(scopes []*yaml.Node, name string, m *yaml.Node) {
	for _, s := range scopes {
		addTo(d.anchors, declKey{s, name}, m)
	}
}

func (d decls) addID(scopes []*yaml.Node, key uriKey, m *yaml.Node) {
	for _, s := range scopes {
		addTo(d.allIDs, s, m)
		if key.any {
			addTo(d.anyIDs, s, m)
			continue
		}
		addTo(d.idsBySegment, declKey{s, key.segment}, m)
	}
}

// identified is the sets of mappings in scope's schema document whose $id could
// name the same resource as want under some base: every $id for a reference
// with no path of its own, else those sharing its last segment and those with
// no path of their own.
func (d decls) identified(scope *yaml.Node, want uriKey) []*posSet {
	if want.any {
		return []*posSet{d.allIDs[scope]}
	}
	return []*posSet{d.idsBySegment[declKey{scope, want.segment}], d.anyIDs[scope]}
}

// uriKey is what no base can change in a URI: the last segment of its path
// once the library has normalized it. any is a URI with no path of its own,
// which takes its base's and so can match every other.
type uriKey struct {
	segment string
	any     bool
}

// keyOfURI reads a $id or a reference's URI part as the registry does: the
// fragment is dropped, and the rest resolved and re-encoded by ResolveURI. A
// '.' or '..' segment or a percent-encoded character changes the last segment
// a raw reading would take, so it is the library's reading that is kept.
func keyOfURI(uri string) uriKey {
	uri, _, _ = strings.Cut(uri, "#")
	if u, err := url.Parse(uri); err == nil && u.Scheme == "" && u.Host == "" && u.Path == "" {
		return uriKey{any: true}
	}
	return uriKey{segment: lastSegment(oas3.ResolveURI(reachBase, uri))}
}

// lastSegment is the final path segment of a URI, the part no base can change.
func lastSegment(uri string) string {
	uri, _, _ = strings.Cut(uri, "#")
	uri, _, _ = strings.Cut(uri, "?")
	if i := strings.LastIndexByte(uri, '/'); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

func pointerTokens(pointer string) []string {
	var out []string
	for t := range jsontext.Pointer(pointer).Tokens() {
		out = append(out, t)
	}
	return out
}
