package bind

import (
	"encoding/json/jsontext"
	"net/url"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// defsPrefix opens every pointer read relative to the schema spelling it.
const defsPrefix = "/$defs/"

// defs is where a "#/$defs/..." pointer written at at lands, by the rule
// internal/defs documents (defs.Reader.Target): at's own definitions when it
// is a schema resource of its own, else the position the pointer names from
// the document's root, else the definitions of the nearest schema above at
// holding a schema there. Unlike the parser's resolver, the answer depends on
// nothing read before, so the root's definition answers even where at defines
// one of its own (docs/parser-library.md#root-defs).
func (b *Binder) defs(at, pointer jsontext.Pointer) (jsontext.Pointer, bool) {
	if key, single := singleKey(pointer); single && b.ownResource(at) {
		if own := at + defsRootOf(key); b.at(own) != nil {
			return own, true
		}
	}
	if r := roleOf(tokens(pointer)); (r == Schema || r == Raw) && b.at(pointer) != nil {
		return pointer, true
	}
	for anc := at.Parent(); anc != ""; anc = anc.Parent() {
		if !b.spend(1) {
			return "", false
		}
		if roleOf(tokens(anc)) == Schema && roleOf(tokens(anc+pointer)) == Schema && b.at(anc+pointer) != nil {
			return anc + pointer, true
		}
	}
	return "", false
}

// defsRootOf is the pointer "/$defs/key".
func defsRootOf(key string) jsontext.Pointer {
	return jsontext.Pointer("/$defs").AppendToken(key)
}

// singleKey is the one key "/$defs/key" names, and whether pointer names one
// key with no path beyond it. A trailing empty token is read as no token, as
// internal/defs reads it (GitHub #843).
func singleKey(pointer jsontext.Pointer) (string, bool) {
	ts := tokens(pointer)[1:]
	if len(ts) == 2 && ts[1] == "" {
		ts = ts[:1]
	}
	if len(ts) != 1 {
		return "", false
	}
	return ts[0], true
}

// base is a schema position's resource: the base URI its $id chain resolves
// to, nil where no $id is written above it, that of the outermost schema
// holding it, and whether it writes an $id of its own.
type base struct {
	effective, top *url.URL
	ownID          bool
}

// ownResource reports whether the schema at at is a resource of its own, as
// the parser reads one at decoding: it writes an $id, or sits in a resource
// whose base differs from that of the outermost schema holding it.
func (b *Binder) ownResource(at jsontext.Pointer) bool {
	r := b.baseOf(at)
	return r.ownID || spell(r.effective) != spell(r.top)
}

// spell is u written out, or "" for none.
func spell(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}

// baseOf is at's resource, read down from the outermost schema holding it and
// remembered per position, so the schemas above at are read once however many
// positions below them ask.
func (b *Binder) baseOf(at jsontext.Pointer) base {
	var chain []jsontext.Pointer
	for here, top := at, false; !top; here, top = b.schemaParent(here) {
		if _, known := b.bases[here]; known {
			break
		}
		chain = append(chain, here)
	}
	for _, here := range slices.Backward(chain) {
		parent, top := b.schemaParent(here)
		r := b.bases[parent]
		id, written := b.idOf(b.at(here))
		r.ownID = written
		if written && !strings.Contains(id, "#") {
			r.effective = rebase(r.effective, id)
		}
		if top {
			r.top = r.effective
		}
		b.bases[here] = r
	}
	return b.bases[at]
}

// schemaParent is the nearest schema position above at, and whether there is
// none, which makes at the outermost schema of its resource tree.
func (b *Binder) schemaParent(at jsontext.Pointer) (jsontext.Pointer, bool) {
	for anc := at.Parent(); anc != ""; anc = anc.Parent() {
		if roleOf(tokens(anc)) == Schema {
			return anc, false
		}
	}
	return "", true
}

// idOf is the $id n writes, and whether it writes a non-empty string one.
func (b *Binder) idOf(n *yaml.Node) (string, bool) {
	v := nodeview.Deref(b.view.ChildByToken(nodeview.Deref(n), "$id"))
	if v == nil || v.Kind != yaml.ScalarNode || v.Value == "" {
		return "", false
	}
	return v.Value, true
}

// rebase is the base an $id of id makes of base, resolved as RFC 3986 does: id
// itself where no base is written above it. An id that is no URI reference
// leaves the base as it was.
func rebase(base *url.URL, id string) *url.URL {
	ref, err := url.Parse(id)
	switch {
	case err != nil:
		return base
	case base == nil:
		return ref
	}
	return base.ResolveReference(ref)
}
