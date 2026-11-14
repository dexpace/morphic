// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// reader reads what a walk needs off a document, w, which must be whole: what
// a model's members are, and where a body holds a page, each read once however
// many operations or members name it.
type reader struct {
	w      *whole.Document
	models map[ir.TypeID]*model
	pages  map[pageKey]page
}

// newReader returns a reader over w that has read nothing yet.
func newReader(w *whole.Document) *reader {
	return &reader{w: w, models: map[ir.TypeID]*model{}, pages: map[pageKey]page{}}
}

// model is what a page reads of one model's members across its composition
// (ir.FlattenedProperties), each as the declaration its flattening takes,
// which is the type a page decodes it as: those that are lists, those that are
// models, with the model, those of a type a walk sends and the urls, each by
// canonical name, and the booleans.
type model struct {
	lists   []*ir.Property
	objects []object
	scalars map[string][]scalar
	urls    map[string][]*ir.Property
	flags   []*ir.Property
}

// object is a member whose type is the model id.
type object struct {
	member *ir.Property
	id     ir.TypeID
}

// scalar is a member whose type is the primitive prim, a walk sending it.
type scalar struct {
	member *ir.Property
	prim   ir.PrimKind
}

// pageKey is a body read for a walk: its type, for a walk reading a next
// cursor the canonical name and the primitive of the place it feeds, which
// name and type the member carrying it, and whether the walk is a POST's,
// whose page names its items at its root and its next cursor as next_<cursor>
// alone, as the answer of a create need not.
type pageKey struct {
	body   ir.TypeID
	cursor string
	prim   ir.PrimKind
	strict bool
}

// page is where a body holds what a walk reads, each a path of members from
// the body's root: its items, none for the body whole, and its next cursor,
// flag, totals and links, nil where it states none. why says, where it is set,
// what the body is or holds that leaves it no page, and the paths are then
// unset. nextWhy says why it holds no next cursor, which a response header may
// carry instead, and nextHeader that next names that header.
type page struct {
	items, next, hasMore, totalCount, totalPages []ir.PropID
	nextLink, prevLink                           []ir.PropID
	why, nextWhy                                 string
	nextHeader                                   bool
}

// equal reports whether p and q, two pages a body holds, hold them at the same
// members. Their links may differ: such a link is left unset (agreed), which
// stops no walk.
func (p page) equal(q page) bool {
	return slices.Equal(p.items, q.items) && slices.Equal(p.next, q.next) && slices.Equal(p.hasMore, q.hasMore) &&
		slices.Equal(p.totalCount, q.totalCount) && slices.Equal(p.totalPages, q.totalPages)
}

// responsePage returns where op's success responses hold the page a walk
// sending in reads. Every content of every one must hold it at the same
// members, since a pager reads whichever arrives; one with no body, a 204 or a
// 304, holds none, as a pager ends at it as at an empty page
// (docs/emitters/plan/responses.md#other-responses), and a content that
// streams is no page (docs/ir/payloads.md#a-content-that-streams). why says
// otherwise what stops it: of several bodies holding no page, the least of
// their reasons, so the finding reads the same whatever order the responses
// and contents take.
func (r *reader) responsePage(op *ir.Operation, in inputs) (page, string) {
	if len(op.Responses) == 0 {
		return page{}, "the operation declares no success response to read a page from"
	}
	key := pageKey{strict: in.strict}
	var header *ir.PropPath
	var headerName string
	if in.readsNextCursor() {
		key.cursor, key.prim = in.cursor.canonical(), in.prim
		if !in.strict {
			header, headerName = headerCursor(r.w, op, key.cursor)
		}
	}
	var pages []page
	var whys []string
	for ct := range pageContents(op) {
		key.body = ct.Type.Target
		pg := r.page(key)
		switch {
		case pg.why != "":
			whys = append(whys, fmt.Sprintf("the %s response body %s", ct.MediaType, pg.why))
		case pg.nextWhy != "" && header == nil:
			whys = append(whys, fmt.Sprintf("the %s response body %s", ct.MediaType, pg.nextWhy))
		default:
			pages = append(pages, pg)
		}
	}
	switch {
	case len(whys) > 0:
		return page{}, slices.Min(whys)
	case len(pages) == 0:
		return page{}, "no success response has a body to read a page from"
	case slices.ContainsFunc(pages[1:], func(pg page) bool { return !pages[0].equal(pg) }):
		return page{}, "the success responses' bodies hold their pages at different members"
	}
	return withExtras(pages, header, headerName)
}

// withExtras returns the page pages, two or more holding it at the same
// members, hold, with the links each holds alike and the next cursor header
// carries, header being named name, where it is not nil. A next cursor the
// body names as well leaves the walk undecided: a pager cannot tell which to
// follow.
func withExtras(pages []page, header *ir.PropPath, name string) (page, string) {
	out := pages[0]
	out.nextLink = agreed(pages, func(pg page) []ir.PropID { return pg.nextLink })
	out.prevLink = agreed(pages, func(pg page) []ir.PropID { return pg.prevLink })
	if header == nil {
		return out, ""
	}
	if out.next != nil {
		return page{}, fmt.Sprintf("the operation names its next cursor in the body and in response header %q", name)
	}
	out.next, out.nextHeader, out.nextWhy = header.Segments, true, ""
	return out, ""
}

// page returns where the body key names holds a page, read once per key.
func (r *reader) page(key pageKey) page {
	if pg, read := r.pages[key]; read {
		return pg
	}
	pg := r.readPage(key)
	r.pages[key] = pg
	return pg
}

// readPage reads where the body key names holds a page: a list holds its items
// whole and nothing more, and a model holds them at a member.
func (r *reader) readPage(key pageKey) page {
	switch td := r.w.Stands(key.body).(type) {
	case *ir.List:
		if key.cursor != "" {
			return page{nextWhy: "is a list, which holds no next cursor"}
		}
		return page{}
	case *ir.Model:
		return r.modelPage(r.model(td.ID), key)
	default:
		return page{why: "is neither an object nor a list"}
	}
}

// modelPage reads where the model body holds a page: its items, its next
// cursor where key names one, and its totals.
func (r *reader) modelPage(body *model, key pageKey) page {
	items, why := r.items(body, key.strict)
	if why != "" {
		return page{why: why}
	}
	pg := page{items: items, nextLink: r.link(body, true, nextLinkNames()), prevLink: r.link(body, true, prevLinkNames())}
	if key.cursor != "" {
		next, holder, notNext, none := r.nextCursor(body, key)
		if notNext != "" && !none {
			return page{why: notNext}
		}
		flag, notFlag := r.flag(body, holder)
		if notFlag != "" {
			return page{why: notFlag}
		}
		pg.next, pg.nextWhy, pg.hasMore = next, notNext, flag
	}
	pg.totalCount = r.total(body, "total_count")
	pg.totalPages = r.total(body, "total_pages")
	return pg
}

// nextCursor returns the path to the one member of body, or of a model one of
// its members holds, of the cursor's primitive and named as the cursor with
// next before it; where none is and key is not strict, as the cursor itself,
// or, for a cursor that is no string, next: a page naming both echoes the
// cursor it was sent beside the next one, and a create answers with what it
// was sent. It returns too the model holding that member, and otherwise why
// there is none, none reporting that the body names no next cursor, which a
// header may carry instead.
func (r *reader) nextCursor(body *model, key pageKey) (path []ir.PropID, holder *model, why string, none bool) {
	names := []string{"next_" + key.cursor, key.cursor, "next"}
	switch {
	case key.strict:
		names = names[:1]
	case key.prim == ir.PrimString:
		names = names[:2]
	}
	for _, name := range names {
		path, holder, found := r.find(body, name, func(prim ir.PrimKind) bool { return prim == key.prim })
		switch found {
		case 0:
			continue
		case 1:
			return path, holder, "", false
		default:
			return nil, nil, fmt.Sprintf("holds %d members %q of the cursor's type, each a next cursor", found, name), false
		}
	}
	return nil, nil, r.noNextCursor(body, key, names[0]), true
}

// noNextCursor says why body holds no next cursor for key, next being the name
// it is first read under: a plain string next beside a string cursor may be
// that cursor or a link, which leaves the walk undecided.
func (r *reader) noNextCursor(body *model, key pageKey, next string) string {
	switch {
	case key.strict:
		return fmt.Sprintf("holds no member %q of the cursor's type, which a POST read as a walk names its next "+
			"cursor, never the cursor it was sent, as a create answers with what it was sent", next)
	case key.prim == ir.PrimString:
		if _, _, found := r.find(body, "next", func(prim ir.PrimKind) bool { return prim == ir.PrimString }); found > 0 {
			return `holds "next", a plain string that may be its next cursor or a link`
		}
	}
	return fmt.Sprintf("holds no member %q or %q of the cursor's type", next, key.cursor)
}

// flag returns the path to the one boolean member of body, or of a model one
// of its members holds, named as a flag (flagName), which ends a cursor walk
// (ir.PageStopNoMore), and nil where none is. Flags are counted by member, so
// one name at two places is two, and why then says the page is undecided, as
// it does where holder, the model holding the next cursor, holds a boolean
// under another name: `truncated` may say more follows or that a result was cut
// short for good, and a pager ending there would drop pages.
func (r *reader) flag(body, holder *model) ([]ir.PropID, string) {
	var paths [][]ir.PropID
	var names []string
	for _, f := range body.flags {
		if flagName(f.Name.Canonical) {
			paths, names = append(paths, []ir.PropID{f.ID}), append(names, strconv.Quote(f.EffectiveWireName()))
		}
	}
	for _, o := range body.objects {
		for _, f := range r.model(o.id).flags {
			if flagName(f.Name.Canonical) {
				paths = append(paths, []ir.PropID{o.member.ID, f.ID})
				names = append(names, strconv.Quote(o.member.EffectiveWireName()+"."+f.EffectiveWireName()))
			}
		}
	}
	switch {
	case len(paths) == 1:
		return paths[0], ""
	case len(paths) > 1:
		return nil, fmt.Sprintf("holds flags %s, each able to end the walk", listed(names))
	}
	if least := leastFlag(holder); least != "" {
		return nil, fmt.Sprintf("holds %q beside its next cursor, a boolean that may end the walk under a name "+
			"the policy does not read", least)
	}
	return nil, ""
}

// flagName reports whether a member's canonical name names a page's flag that
// another page follows, as docs/compilers/openapi.md#pagination-policy lists.
func flagName(canonical string) bool {
	switch canonical {
	case "has_more", "has_next", "has_next_page", "is_truncated":
		return true
	default:
		return false
	}
}

// leastFlag returns the least wire name of the booleans m holds, or "" for
// none or a nil m, so a finding naming one reads the same in any order.
func leastFlag(m *model) string {
	least := ""
	if m == nil {
		return least
	}
	for _, f := range m.flags {
		if name := f.EffectiveWireName(); least == "" || name < least {
			least = name
		}
	}
	return least
}

// total returns the path to the one member named name, of a type a walk counts
// with, or nil where the body holds none or several.
func (r *reader) total(body *model, name string) []ir.PropID {
	path, _, found := r.find(body, name, func(ir.PrimKind) bool { return true })
	if found != 1 {
		return nil
	}
	return path
}

// find returns how many members named name, of body or of a model one of its
// members holds, are of a primitive a walk sends that admit accepts, and the
// path to one of them and the model holding it, which are the only ones when
// that is one.
func (r *reader) find(body *model, name string, admit func(ir.PrimKind) bool) ([]ir.PropID, *model, int) {
	var path []ir.PropID
	var holder *model
	found := 0
	for _, s := range body.scalars[name] {
		if admit(s.prim) {
			path, holder, found = []ir.PropID{s.member.ID}, body, found+1
		}
	}
	for _, o := range body.objects {
		held := r.model(o.id)
		for _, s := range held.scalars[name] {
			if admit(s.prim) {
				path, holder, found = []ir.PropID{o.member.ID, s.member.ID}, held, found+1
			}
		}
	}
	return path, holder, found
}

// items returns the path to the list body holds a page's items in: the one
// list named as items; else, where strict is not set, the one list the one
// model named so holds, or the body's one list. why says otherwise what stops
// it.
func (r *reader) items(body *model, strict bool) ([]ir.PropID, string) {
	var named, holders []*ir.Property
	var held ir.TypeID
	for _, l := range body.lists {
		if itemsName(l.Name.Canonical) {
			named = append(named, l)
		}
	}
	for _, o := range body.objects {
		if itemsName(o.member.Name.Canonical) {
			holders, held = append(holders, o.member), o.id
		}
	}
	switch {
	case len(named) == 1:
		return []ir.PropID{named[0].ID}, ""
	case len(named) > 1:
		return nil, fmt.Sprintf("holds lists %s, each named as its items", wireNames(named))
	case strict:
		return nil, "holds no list named as its items at its root, where a POST read as a walk holds them, as a " +
			"create's answer may hold a list of another kind"
	case len(holders) == 1:
		return heldItems(holders[0], r.model(held))
	case len(holders) > 1:
		return nil, fmt.Sprintf("holds objects %s, each named as holding its items", wireNames(holders))
	case len(body.lists) == 1:
		return []ir.PropID{body.lists[0].ID}, ""
	case len(body.lists) == 0:
		return nil, "holds no list of items"
	default:
		return nil, fmt.Sprintf("holds lists %s and names none as its items", wireNames(body.lists))
	}
}

// heldItems returns the path to the one list the member holder holds, its
// type being the model held.
func heldItems(holder *ir.Property, held *model) ([]ir.PropID, string) {
	switch len(held.lists) {
	case 1:
		return []ir.PropID{holder.ID, held.lists[0].ID}, ""
	case 0:
		return nil, fmt.Sprintf("holds %q, which holds no list of items", holder.EffectiveWireName())
	default:
		return nil, fmt.Sprintf("holds %q, which holds lists %s and names none as its items",
			holder.EffectiveWireName(), wireNames(held.lists))
	}
}

// itemsName reports whether a member's canonical name names where a page holds
// its items, as docs/compilers/openapi.md#pagination-policy lists.
func itemsName(canonical string) bool {
	switch canonical {
	case "result", "results", "data", "items", "records", "entries", "values":
		return true
	default:
		return false
	}
}

// model returns what a page reads of the members of the model id, read once
// per model.
func (r *reader) model(id ir.TypeID) *model {
	if m, read := r.models[id]; read {
		return m
	}
	m := &model{scalars: map[string][]scalar{}, urls: map[string][]*ir.Property{}}
	for _, p := range r.w.Members(id) {
		switch td := r.w.Stands(p.Type.Target).(type) {
		case *ir.List:
			m.lists = append(m.lists, p)
		case *ir.Model:
			m.objects = append(m.objects, object{member: p, id: td.ID})
		default:
			if prim, ok := sendableAs(td); ok {
				m.scalars[p.Name.Canonical] = append(m.scalars[p.Name.Canonical], scalar{member: p, prim: prim})
			} else if isBool(td) {
				m.flags = append(m.flags, p)
			} else if isURL(td) {
				m.urls[p.Name.Canonical] = append(m.urls[p.Name.Canonical], p)
			}
		}
	}
	r.models[id] = m
	return m
}

// isBool reports whether td is the boolean primitive.
func isBool(td ir.TypeDef) bool {
	p, ok := td.(*ir.Primitive)
	return ok && p.Prim == ir.PrimBool
}

// wireNames lists the members' wire names, two or more, as listed does.
func wireNames(members []*ir.Property) string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = strconv.Quote(m.EffectiveWireName())
	}
	return listed(names)
}
