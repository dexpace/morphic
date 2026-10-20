package bind

import (
	"encoding/json/jsontext"
	"net/url"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// defsRoot is the position a schema's definitions sit at, and defsPrefix opens
// every pointer the $defs rule reads.
const (
	defsRoot   jsontext.Pointer = "/$defs"
	defsPrefix                  = string(defsRoot) + "/"
)

// IsDefs reports whether pointer is one the $defs rule reads, from where the
// $ref spelling it is written rather than from its document's root alone.
func IsDefs(pointer jsontext.Pointer) bool {
	return strings.HasPrefix(string(pointer), defsPrefix)
}

// definitions reads "#/$defs/..." pointers by the $defs rule, over a table's
// documents and within its budget.
type definitions struct {
	docs *docs
	work *budget
}

// definition is where a "#/$defs/..." pointer written at site lands, by the
// rule docs/compilers/openapi.md#references states: site's own definitions
// when it is a schema resource of its own and own allows them, else the
// position the pointer names from the root of site's document, else the
// definitions of the nearest object above site that holds "$defs" in the
// model. The answer depends on nothing read before, unlike the parser's
// resolver (docs/parser-library.md#defs-order), nor does the root's answer
// give way to a schema's own (docs/parser-library.md#root-defs).
//
// A read the merge bound stopped answers MergeBound: what it missed may
// overrule a later answer.
func (d definitions) definition(site Position, pointer jsontext.Pointer, own bool) (reading, Reason) {
	unowned := false
	if key, single := singleKey(pointer); own && single {
		r, reason, settled := d.ownDefinition(site, key)
		if settled {
			return r, reason
		}
		unowned = reason == OwnDefinition
	}
	r, reason := d.docs.read(site.Doc, pointer)
	switch {
	case reason == NoReason && answers(r):
		return r, NoReason
	case reason == MergeBound:
		return reading{}, MergeBound
	}
	r, why := d.ancestorDefinition(site, pointer)
	if unowned && (why == NoDefinition || why == RawDefinition) {
		why = OwnDefinition // the nearer definition left unread
	}
	return r, why
}

// ownDefinition is the definition site's own definitions give key, and whether
// that settles the answer: it does where site is a schema resource of its own
// whose definitions hold key, and where the merge bound hides either key from
// them or an $id that decides whether site is a resource of its own. Where
// they hold key and site is no resource of its own, the reason is
// OwnDefinition, and the answer is left to the rest of the rule.
func (d definitions) ownDefinition(site Position, key string) (reading, Reason, bool) {
	// A site is a position its document holds, so the path reaches it.
	path, _ := d.docs.path(site)
	resource, known := d.ownResource(site, path)
	n, past := d.below(site.Doc, path[len(path)-1], []string{"$defs", key})
	switch {
	case (resource || !known) && past, !known && schemaShaped(n):
		return reading{}, MergeBound, true
	case resource && schemaShaped(n):
		at := Position{Doc: site.Doc, Pointer: site.Pointer + defsRoot.AppendToken(key)}
		return reading{at: at, node: n, state: d.docs.stateAt(at)}, NoReason, true
	case schemaShaped(n):
		return reading{}, OwnDefinition, false
	}
	return reading{}, NoReason, false
}

// ancestorDefinition is the definition the nearest object above site that
// holds "$defs" in the model gives pointer, read on into raw YAML where the
// model leaves it, as the parser's resolver reads it (GitHub #997). The root
// is not such an object: definition has read it already. A read the merge
// bound stops makes it MergeBound, as a farther object would answer only
// where that one holds nothing. Where none answers and raw YAML above site
// holds the path, the reason is RawDefinition.
func (d definitions) ancestorDefinition(site Position, pointer jsontext.Pointer) (reading, Reason) {
	if !d.docs.list[site.Doc].grammar {
		return reading{}, NoDefinition
	}
	// A site is a position its document holds, so the path reaches it.
	path, _ := d.docs.path(site)
	tokens := slices.Collect(site.Pointer.Tokens())
	states := make([]state, len(tokens)+1)
	states[0] = sDocument
	for i, t := range tokens {
		states[i+1] = step(states[i], t)
	}
	rest := slices.Collect(pointer.Tokens())
	why := NoDefinition
	for i := len(tokens) - 1; i >= 1; i-- {
		if !d.work.spend(1) {
			break
		}
		if !modelled(states[i]) {
			if why == NoDefinition && d.holds(site.Doc, path[i], rest) {
				why = RawDefinition
			}
			continue
		}
		if !modelled(step(states[i], "$defs")) {
			continue
		}
		n, past := d.below(site.Doc, path[i], rest)
		if r := (reading{node: n, state: stateOf(states[i], rest)}); n != nil && answers(r) {
			r.at = Position{Doc: site.Doc, Pointer: jsontext.Pointer(prefix(site.Pointer, i)) + pointer}
			return r, NoReason
		}
		if past {
			return reading{}, MergeBound
		}
	}
	return reading{}, why
}

// below is the node tokens name from n in document id, or nil, and whether the
// read stopped at a mapping merging past the view's bound (see absent).
func (d definitions) below(id DocID, n *yaml.Node, tokens []string) (*yaml.Node, bool) {
	for _, t := range tokens {
		if !d.work.spend(1) {
			return nil, false
		}
		next := d.docs.child(id, n, t)
		if next == nil {
			return nil, d.docs.absent(id, n) == MergeBound
		}
		n = next
	}
	return n, false
}

// holds reports whether tokens name a node read as a schema below n in
// document id.
func (d definitions) holds(id DocID, n *yaml.Node, tokens []string) bool {
	held, _ := d.below(id, n, tokens)
	return schemaShaped(held)
}

// answers reports whether a definition read at r answers its pointer: a
// schema of the model, or raw YAML read as one.
func answers(r reading) bool {
	role := roleOf(r.state)
	return (role == Schema || role == Raw) && schemaShaped(r.node)
}

// prefix is the pointer to pointer's first n tokens: pointer up to the '/'
// opening the token after them, since an escaped token holds none.
func prefix(pointer jsontext.Pointer, n int) string {
	s := string(pointer)
	for i := range len(s) {
		if s[i] != '/' {
			continue
		}
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// singleKey is the one key "/$defs/key" names, and whether pointer names one
// key with no path beyond it.
func singleKey(pointer jsontext.Pointer) (string, bool) {
	ts := slices.Collect(pointer.Tokens())
	if len(ts) != 2 {
		return "", false
	}
	return ts[1], true
}

// ownResource reports whether the schema at site, reached along path, is a
// schema resource of its own: it writes an $id, or sits in a resource whose
// base differs from that of the outermost schema holding it. A base is each
// $id above it, resolved as RFC 3986 resolves a reference against the one
// before, the first against the document's own; neither a fragment-only $id
// nor one that is no URI reference moves it. Its second result is false where
// a schema on the path writes no $id and merges past the view's bound, which
// may hide one.
func (d definitions) ownResource(site Position, path []*yaml.Node) (bool, bool) {
	view := d.docs.list[site.Doc].view
	// The document's own URI is unknown here and the same for every base, so
	// any absolute one serves.
	base := &url.URL{Scheme: "document", Path: "/"}
	var top *url.URL
	outermost, written, known := true, false, true
	state, tokens := d.docs.rootState(site.Doc), slices.Collect(site.Pointer.Tokens())
	for i, n := range path {
		if i > 0 {
			state = step(state, tokens[i-1])
		}
		if state != sSchema && i < len(path)-1 {
			continue
		}
		var id string
		id, written = d.idOf(site.Doc, n)
		known = known && (written || view.Expanded(n))
		if written && !strings.Contains(id, "#") {
			base = rebase(base, id)
		}
		if outermost {
			top, outermost = base, false
		}
	}
	return written || base.String() != top.String(), known
}

// idOf is the $id n writes, and whether it writes a non-empty string one: a
// null $id is none, as refOf reads a null $ref.
func (d definitions) idOf(id DocID, n *yaml.Node) (string, bool) {
	v := nodeview.Deref(d.docs.list[id].view.ChildByToken(nodeview.Deref(n), "$id"))
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" || v.Value == "" {
		return "", false
	}
	return v.Value, true
}

// rebase is the base an $id of id makes of base, resolved as RFC 3986
// resolves a reference.
func rebase(base *url.URL, id string) *url.URL {
	ref, err := url.Parse(id)
	if err != nil {
		return base
	}
	return base.ResolveReference(ref)
}
