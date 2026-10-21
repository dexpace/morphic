package bind

import (
	"encoding/json/jsontext"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
)

// resolve is where ref, written at site, lands. own says whether the $defs
// rule reads site's own definitions, as it does for a $ref and not for a
// discriminator's mapping value.
func (b *binder) resolve(site Position, ref string, own bool) Hop {
	h := Hop{Ref: ref, Status: Unresolved}
	if _, frag, _ := strings.Cut(ref, "#"); strings.Contains(frag, "#") {
		h.Reason = BadPointer
		return h
	}
	doc, reason, detail := b.document(site, ref)
	if reason != NoReason {
		h.Reason, h.Detail = reason, detail
		return h
	}
	pointer := fragment.Pointer(ref)
	if pointer != "" && !strings.HasPrefix(string(pointer), "/") {
		h.Reason = BadPointer
		if isAnchorName(string(pointer)) {
			h.Reason = AnchorTarget
		}
		return h
	}
	if !pointer.IsValid() || !utf8.ValidString(string(pointer)) {
		h.Reason = BadPointer
		return h
	}
	h.Named = doc == Source
	r, reason := b.land(site, doc, ref, pointer, own, &h)
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

// document is the document ref, written at site, names: site's own when ref
// has no document part, or the one its document part names, opened.
func (b *binder) document(site Position, ref string) (DocID, Reason, string) {
	uri, _, _ := strings.Cut(ref, "#")
	if uri = strings.TrimSpace(uri); uri == "" {
		return site.Doc, NoReason, ""
	}
	if refuse := b.docs.cfg.Refuse; refuse != nil {
		if err := refuse(uri); err != nil {
			return 0, Unreadable, err.Error()
		}
	}
	name, ok := b.docs.locate(uri, b.docs.list[site.Doc].name)
	if !ok {
		return 0, Unreadable, "it names no document a reference can"
	}
	id, err := b.docs.open(name)
	switch {
	case errors.Is(err, errNotRead):
		return 0, ExternalOff, ""
	case err != nil:
		return 0, Unreadable, err.Error()
	}
	return id, NoReason, ""
}

// land reads where pointer, from ref written at site, names in document doc,
// and notes in h the pointer it spells when it names the source: a
// "#/$defs/..." one by the $defs rule (definition), any other from the root.
// Either is refused where it lands in, or is written in, the fields beside a
// path item's $ref: the parser reads such a pointer through that $ref instead
// (GitHub #824).
func (b *binder) land(site Position, doc DocID, ref string, pointer jsontext.Pointer, own bool, h *Hop,
) (reading, Reason) {
	uri, _, _ := strings.Cut(ref, "#")
	if strings.TrimSpace(uri) == "" && isDefs(pointer) {
		if site.Doc == Source && b.beside(site.Pointer, h) {
			return reading{}, BesideDefs
		}
		r, reason := b.defs.definition(site, pointer, own)
		if reason == NoReason && h.Named {
			h.Spelled = r.at.Pointer
		}
		return r, reason
	}
	if h.Named {
		h.Spelled = pointer
		if b.beside(pointer, h) {
			return reading{}, BesideRef
		}
	}
	return b.docs.read(doc, pointer)
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
	ref, _ := b.docs.refOf(p.Doc, b.docs.node(p))
	h := b.resolve(p, ref, true)
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
