// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"errors"
	"strings"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
)

// resolve is where ref, written at site, reached along path, lands: in the
// resource its document part names against the resource site is in
// (resources.holding), at the position its fragment names there. schema says
// whether site is read as a schema whatever the grammar reads it as, as a
// $ref's is and a discriminator's mapping value's is not: its own $id then
// counts, and a "#/$defs/..." pointer nothing answers says whether its own
// definitions hold the name.
func (b *binder) resolve(site Position, path []*yaml.Node, ref string, schema bool) Hop {
	h := Hop{Ref: ref, Status: Unresolved}
	uri, frag, _ := strings.Cut(ref, "#")
	if strings.Contains(frag, "#") {
		h.Reason = BadPointer
		return h
	}
	in, known := b.resources.holding(site, path, schema)
	if !known {
		h.Reason = MergeBound
		return h
	}
	to, reason, detail := b.named(in, uri)
	if reason != NoReason {
		h.Reason, h.Detail = reason, detail
		return h
	}
	h.Named = to.root.Doc == Source
	r, reason := b.land(site, path, to, ref, schema, &h)
	if reason != NoReason {
		h.Reason = reason
		return h
	}
	if !answers(r) {
		h.Reason = NotSchema
		return h
	}
	h.Status, h.Target, h.Role = Resolved, r.at, roleOf(r.state)
	return h
}

// named is the resource the document part uri of a reference read in the
// resource in names: in itself for none, else the one the URI it names against
// in's identifies: a resource an $id names, the source, or the document there,
// opened. A relative URI read against an $id that no schema declares names the
// document at the URI the $id makes of it (readAt); an absolute one names what
// it names wherever it is written.
func (b *binder) named(in resource, uri string) (resource, Reason, string) {
	if uri = strings.TrimSpace(uri); uri == "" {
		return in, NoReason, ""
	}
	key, ok := b.resources.resolveURI(uri, in.uri)
	if !ok {
		return resource{}, Unreadable, "it names no document a reference can"
	}
	if b.resources.names(Source, key) {
		return b.resources.document(Source), NoReason, ""
	}
	// A miss the merge bound may explain is read as a location all the same:
	// the table says it is truncated.
	root, found, why := b.ids.lookupURI(key)
	switch {
	case found:
		return resource{root: root, uri: key, id: true}, NoReason, ""
	case why == DuplicateID:
		return resource{}, why, key
	case in.id && !absolute(uri):
		return b.readAt(in, uri, key)
	}
	return b.opened(in, uri)
}

// opened is the resource of the document uri, written in the resource in,
// names, placed by Config.Locate, or why not: Config.Refuse refuses that
// spelling, it names no document, or the read fails (read). An identifier is
// never refused, since no document is read for one.
func (b *binder) opened(in resource, uri string) (resource, Reason, string) {
	if why := b.docs.refusal(uri); why != "" {
		return resource{}, Unreadable, why
	}
	name, ok := b.docs.locate(uri, in.uri)
	if !ok {
		return resource{}, Unreadable, "it names no document a reference can"
	}
	return b.read(name)
}

// readAt is the resource of the document a relative URI uri names, written in
// the resource in, whose base an $id gives: key, the URI that base makes of it
// by RFC 3986, read as such rather than placed by Config.Locate, which reads a
// path against a URL as a local one (GitHub #936). Config.Refuse judges uri as
// written where only relative paths moved the base, and key once an absolute
// $id detached it, so an $id cannot make a refused read pass as relative.
func (b *binder) readAt(in resource, uri, key string) (resource, Reason, string) {
	spelled := uri
	if in.detached {
		spelled = key
	}
	if why := b.docs.refusal(spelled); why != "" {
		return resource{}, Unreadable, why
	}
	return b.read(key)
}

// read is the resource of the document named name, read through Config.Open,
// or why the read failed.
func (b *binder) read(name string) (resource, Reason, string) {
	id, err := b.docs.open(name)
	var unparsable *UnparsableError
	switch {
	case errors.Is(err, errNotRead):
		return resource{}, ExternalOff, ""
	case errors.As(err, &unparsable):
		return resource{}, Unparsable, unparsable.Err.Error()
	case errors.Is(err, ErrEmptyDocument):
		return resource{}, Empty, ""
	case err != nil:
		return resource{}, Unreadable, err.Error()
	}
	return b.resources.document(id), NoReason, ""
}

// land reads where the fragment of ref, written at site and reached along
// path, names in the resource to: a JSON pointer from its root, a
// "#/$defs/..." one in a document's own resource by the $defs rule
// (definition), or the schema a plain name names (anchored). It notes in h the
// pointer it spells when it names the source.
func (b *binder) land(site Position, path []*yaml.Node, to resource, ref string, schema bool, h *Hop,
) (reading, Reason) {
	pointer := fragment.Pointer(ref)
	switch {
	case pointer != "" && !strings.HasPrefix(string(pointer), "/"):
		if isAnchorName(string(pointer)) {
			return b.anchored(to, string(pointer), h)
		}
		return reading{}, BadPointer
	case !pointer.IsValid() || !utf8.ValidString(string(pointer)):
		return reading{}, BadPointer
	}
	if uri, _, _ := strings.Cut(ref, "#"); strings.TrimSpace(uri) == "" && !to.id && isDefs(pointer) {
		return b.definition(site, path, pointer, schema, h)
	}
	r, reason := b.pointed(to.root.Doc, to.root.Pointer+pointer, h)
	if reason == NoTarget && to.id {
		reason, h.Detail = NoResourceTarget, to.uri
	}
	return r, reason
}

// definition reads a "#/$defs/..." pointer written at site, reached along
// path, in its document's own resource, by the $defs rule, noting in h the
// position it spells. It is refused where site is in the fields beside a path
// item's $ref, as any definition it can name is: the parser reads such a
// pointer through that $ref instead (GitHub #824).
func (b *binder) definition(site Position, path []*yaml.Node, pointer jsontext.Pointer, schema bool, h *Hop,
) (reading, Reason) {
	if site.Doc == Source && b.beside(site.Pointer, h) {
		return reading{}, BesideDefs
	}
	r, reason := b.defs.definition(site, path, pointer, schema)
	if reason == NoReason && h.Named {
		h.Spelled = r.at.Pointer
	}
	return r, reason
}

// pointed reads pointer from the root of document doc, noting in h the
// pointer it spells when it names the source. It is refused where it lands
// in the fields beside a path item's $ref: the parser reads such a pointer
// through that $ref instead (GitHub #824).
func (b *binder) pointed(doc DocID, pointer jsontext.Pointer, h *Hop) (reading, Reason) {
	if h.Named {
		h.Spelled = pointer
		if b.beside(pointer, h) {
			return reading{}, BesideRef
		}
	}
	return b.docs.read(doc, pointer)
}

// anchored reads the schema the resource to declares name in, as its
// $anchor or $dynamicAnchor. Only the source's identifiers are read.
func (b *binder) anchored(to resource, name string, h *Hop) (reading, Reason) {
	if to.root.Doc != Source {
		return reading{}, AnchorUnread
	}
	at, why := b.ids.lookupAnchor(to.root, name)
	if why != NoReason {
		return reading{}, why
	}
	return b.pointed(Source, at.Pointer, h)
}

// beside reports whether pointer is in the fields beside a path item's $ref
// (Config.Beside), noting in h which ones.
func (b *binder) beside(pointer jsontext.Pointer, h *Hop) bool {
	if b.cfg.Beside == nil {
		return false
	}
	use, key, ok := b.cfg.Beside(pointer)
	if ok {
		h.Use, h.Key = use, key
	}
	return ok
}

// hop is the hop of the $ref written at p, read once and remembered. Only a
// position writing a $ref is asked: a site, or a hop's target that writes one.
func (b *binder) hop(p Position) Hop {
	if h, ok := b.hops[p]; ok {
		return h
	}
	// Only a position its document holds writes a $ref, so the path reaches it.
	path, _ := b.docs.path(p)
	ref, _ := b.docs.refOf(p.Doc, path[len(path)-1])
	h := b.resolve(p, path, ref, true)
	if b.work.over() {
		h = Hop{Ref: ref, Status: OverBudget}
	}
	b.hops[p] = h
	if h.Status == Resolved {
		b.find.landed(h)
	}
	return h
}

// isAnchorName reports whether s is a name a $anchor can declare, by JSON
// Schema 2020-12 section 8.2.2: a letter or '_', then letters, digits, '-',
// '_' and '.'.
func isAnchorName(s string) bool {
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case i > 0 && (c >= '0' && c <= '9' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return s != ""
}
