// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/ir"
)

// reader reads what a walk needs off a document, w, which must be whole: what
// a model's members are, and where a body holds a page, each read once however
// many operations or members name it.
type reader struct {
	w      *compileddoc.CompiledDocumentView
	models *models
	pages  map[pageKey]page
}

// newReader returns a reader over compiledDocumentView that has read nothing
// yet.
func newReader(compiledDocumentView *compileddoc.CompiledDocumentView) *reader {
	return &reader{w: compiledDocumentView, models: &models{w: compiledDocumentView, read: map[ir.TypeID]*model{}}, pages: map[pageKey]page{}}
}

// models is what a page reads of each model of w, read once per model (of),
// which the readers of a page's items, next cursor, flag, totals and links
// take.
type models struct {
	w    *compileddoc.CompiledDocumentView
	read map[ir.TypeID]*model
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

// scalar is a member whose type is the primitive kind primitive, a walk sending
// it.
type scalar struct {
	member    *ir.Property
	primitive ir.PrimitiveKind
}

// pageKey is a body read for a walk: its type, for a walk reading a next
// cursor the canonical name and the primitive of the place it feeds, which
// name and type the member carrying it, and whether the walk is a POST's,
// whose page names its items at its root and its next cursor as next_<cursor>
// alone, as the answer of a create need not.
type pageKey struct {
	body      ir.TypeID
	cursor    string
	primitive ir.PrimitiveKind
	strict    bool
	// link reads the body as a link walk's page (reader.linkPage).
	link bool
}

// page is where a body holds what a walk reads, each a path of members from
// the body's root: its items, none for the body whole, and its next cursor,
// flag, totals and links, nil where it states none. why says, where it is set,
// what the body is or holds that leaves it no page, and the paths are then
// unset. nextWhy says why it holds no next cursor, which a response header may
// carry instead, and nextHeader that next names that header.
type page struct {
	items, next, hasMore, totalCount, totalPages []ir.PropertyID
	nextLink, prevLink                           []ir.PropertyID
	why, nextWhy                                 string
	nextHeader                                   bool
}

// equal reports whether p and pageValue, two pages a body holds, hold them at
// the same members. Their links may differ: such a link is left unset (agreed),
// which stops no walk.
func (p page) equal(pageValue page) bool {
	return slices.Equal(p.items, pageValue.items) && slices.Equal(p.next, pageValue.next) && slices.Equal(p.hasMore, pageValue.hasMore) &&
		slices.Equal(p.totalCount, pageValue.totalCount) && slices.Equal(p.totalPages, pageValue.totalPages)
}

// responsePage returns where operation's success responses hold the page a walk
// sending in reads. Every content of every one must hold it at the same
// members, since a pager reads whichever arrives; one with no body, a 204 or a
// 304, holds none, as a pager ends at it as at an empty page
// (docs/emitters/plan/responses.md#other-responses), and a content that
// streams is no page (docs/ir/payloads.md#a-content-that-streams). why says
// otherwise what stops it: of several bodies holding no page, the least of
// their reasons, so the finding reads the same whatever order the responses
// and contents take.
func (r *reader) responsePage(operation *ir.Operation, in inputs) (page, string) {
	if len(operation.Responses) == 0 {
		return page{}, "the operation declares no success response to read a page from"
	}
	key := pageKey{strict: in.strict}
	var header *ir.PropertyPath
	var headerName string
	if in.readsNextCursor() {
		key.cursor, key.primitive = in.cursor.canonical(), in.primitive
		if !in.strict {
			header, headerName = headerCursor(r.w, operation, key.cursor)
		}
	}
	var pages []page
	var whys []string
	for ct := range pageContents(operation) {
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
func withExtras(pages []page, header *ir.PropertyPath, name string) (page, string) {
	out := pages[0]
	out.nextLink = agreed(pages, func(pg page) []ir.PropertyID { return pg.nextLink })
	out.prevLink = agreed(pages, func(pg page) []ir.PropertyID { return pg.prevLink })
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
		if key.link {
			return r.linkPage(td.ID)
		}
		return modelPage(r.models, r.models.of(td.ID), key)
	default:
		return page{why: "is neither an object nor a list"}
	}
}

// modelPage reads where the model body holds a page: its items, its next
// cursor where key names one, and its totals.
func modelPage(ms *models, body *model, key pageKey) page {
	items, why := pageItems(ms, body, key.strict)
	if why != "" {
		return page{why: why}
	}
	pg := page{items: items, nextLink: pageLink(ms, body, true, nextLinkNames()),
		prevLink: pageLink(ms, body, true, prevLinkNames())}
	if key.cursor != "" {
		next, holder, notNext, none := nextCursor(ms, body, key)
		if notNext != "" && !none {
			return page{why: notNext}
		}
		flag, notFlag := pageFlag(ms, body, holder)
		if notFlag != "" {
			return page{why: notFlag}
		}
		pg.next, pg.nextWhy, pg.hasMore = next, notNext, flag
	}
	pg.totalCount = pageTotal(ms, body, "total_count")
	pg.totalPages = pageTotal(ms, body, "total_pages")
	return pg
}

// nextCursor returns the path to the one member of body, or of a model one of
// its members holds, of the cursor's primitive and named as the cursor with
// next before it; where none is and key is not strict, as the cursor itself,
// or, for a cursor that is no string, next: a page naming both echoes the
// cursor it was sent, and a create answers with what it was sent. It returns
// the model holding that member, and otherwise why there is none, none
// reporting that the body names none, which a header may carry instead.
func nextCursor(ms *models, body *model, key pageKey) (path []ir.PropertyID, holder *model, why string, none bool) {
	names := []string{"next_" + key.cursor, key.cursor, "next"}
	switch {
	case key.strict:
		names = names[:1]
	case key.primitive == ir.PrimitiveString:
		names = names[:2]
	}
	for _, name := range names {
		path, holder, found := find(ms, body, name, func(primitive ir.PrimitiveKind) bool { return primitive == key.primitive })
		switch found {
		case 0:
			continue
		case 1:
			return path, holder, "", false
		default:
			return nil, nil, fmt.Sprintf("holds %d members %q of the cursor's type, each a next cursor", found, name), false
		}
	}
	return nil, nil, noNextCursor(ms, body, key, names[0]), true
}

// noNextCursor says why body holds no next cursor for key, next being the name
// it is first read under: a plain string next beside a string cursor may be
// that cursor or a link, which leaves the walk undecided.
func noNextCursor(ms *models, body *model, key pageKey, next string) string {
	switch {
	case key.strict:
		return fmt.Sprintf("holds no member %q of the cursor's type, which a POST read as a walk names its next "+
			"cursor, never the cursor it was sent, as a create answers with what it was sent", next)
	case key.primitive == ir.PrimitiveString:
		if _, _, found := find(ms, body, "next", func(primitive ir.PrimitiveKind) bool { return primitive == ir.PrimitiveString }); found > 0 {
			return `holds "next", a plain string that may be its next cursor or a link`
		}
	}
	return fmt.Sprintf("holds no member %q or %q of the cursor's type", next, key.cursor)
}

// pageFlag returns the path to the one boolean member of body, or of a model one
// of its members holds, named as a flag (flagName), which ends a cursor walk
// (ir.PageStopNoMore), and nil where none is. Flags are counted by member, so
// one name at two places is two, and why then says the page is undecided, as
// it does where holder, the model holding the next cursor, holds a boolean
// under another name: `truncated` may say more follows or that a result was cut
// short for good, and a pager ending there would drop pages.
func pageFlag(ms *models, body, holder *model) ([]ir.PropertyID, string) {
	var paths [][]ir.PropertyID
	var names []string
	for _, property := range body.flags {
		if flagName(property.Name.Canonical) {
			paths, names = append(paths, []ir.PropertyID{property.ID}), append(names, strconv.Quote(property.EffectiveWireName()))
		}
	}
	for _, object := range body.objects {
		for _, property := range ms.of(object.id).flags {
			if flagName(property.Name.Canonical) {
				paths = append(paths, []ir.PropertyID{object.member.ID, property.ID})
				names = append(names, strconv.Quote(object.member.EffectiveWireName()+"."+property.EffectiveWireName()))
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

// leastFlag returns the least wire name of the booleans modelValue holds, or ""
// for none or a nil modelValue, so a finding naming one reads the same in any
// order.
func leastFlag(modelValue *model) string {
	least := ""
	if modelValue == nil {
		return least
	}
	for _, property := range modelValue.flags {
		if name := property.EffectiveWireName(); least == "" || name < least {
			least = name
		}
	}
	return least
}

// pageTotal returns the path to the one member named name, of a type a walk counts
// with, or nil where the body holds none or several.
func pageTotal(ms *models, body *model, name string) []ir.PropertyID {
	path, _, found := find(ms, body, name, func(ir.PrimitiveKind) bool { return true })
	if found != 1 {
		return nil
	}
	return path
}

// find returns how many members named name, of body or of a model one of its
// members holds, are of a primitive a walk sends that admit accepts, and the
// path to one of them and the model holding it, which are the only ones when
// that is one.
func find(ms *models, body *model, name string, admit func(ir.PrimitiveKind) bool) ([]ir.PropertyID, *model, int) {
	var path []ir.PropertyID
	var holder *model
	found := 0
	for _, scalar := range body.scalars[name] {
		if admit(scalar.primitive) {
			path, holder, found = []ir.PropertyID{scalar.member.ID}, body, found+1
		}
	}
	for _, object := range body.objects {
		held := ms.of(object.id)
		for _, scalar := range held.scalars[name] {
			if admit(scalar.primitive) {
				path, holder, found = []ir.PropertyID{object.member.ID, scalar.member.ID}, held, found+1
			}
		}
	}
	return path, holder, found
}

// pageItems returns the path to the list body holds a page's items in: the one
// list named as items; else, where strict is not set, the one list the one
// model named so holds, or the body's one list. why says otherwise what stops
// it.
func pageItems(ms *models, body *model, strict bool) ([]ir.PropertyID, string) {
	var named, holders []*ir.Property
	var held ir.TypeID
	for _, property := range body.lists {
		if itemsName(property.Name.Canonical) {
			named = append(named, property)
		}
	}
	for _, object := range body.objects {
		if itemsName(object.member.Name.Canonical) {
			holders, held = append(holders, object.member), object.id
		}
	}
	switch {
	case len(named) == 1:
		return []ir.PropertyID{named[0].ID}, ""
	case len(named) > 1:
		return nil, fmt.Sprintf("holds lists %s, each named as its items", wireNames(named))
	case strict:
		return nil, "holds no list named as its items at its root, where a POST read as a walk holds them, as a " +
			"create's answer may hold a list of another kind"
	case len(holders) == 1:
		return heldItems(holders[0], ms.of(held))
	case len(holders) > 1:
		return nil, fmt.Sprintf("holds objects %s, each named as holding its items", wireNames(holders))
	case len(body.lists) == 1:
		return []ir.PropertyID{body.lists[0].ID}, ""
	case len(body.lists) == 0:
		return nil, "holds no list of items"
	default:
		return nil, fmt.Sprintf("holds lists %s and names none as its items", wireNames(body.lists))
	}
}

// heldItems returns the path to the one list the member holder holds, its
// type being the model held.
func heldItems(holder *ir.Property, held *model) ([]ir.PropertyID, string) {
	switch len(held.lists) {
	case 1:
		return []ir.PropertyID{holder.ID, held.lists[0].ID}, ""
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

// of returns what a page reads of the members of the model id, read once
// per model.
func (ms *models) of(id ir.TypeID) *model {
	if model, read := ms.read[id]; read {
		return model
	}
	modelValue := &model{scalars: map[string][]scalar{}, urls: map[string][]*ir.Property{}}
	for _, property := range ms.w.Members(id) {
		switch td := ms.w.Stands(property.Type.Target).(type) {
		case *ir.List:
			modelValue.lists = append(modelValue.lists, property)
		case *ir.Model:
			modelValue.objects = append(modelValue.objects, object{member: property, id: td.ID})
		default:
			if primitive, ok := sendableAs(td); ok {
				modelValue.scalars[property.Name.Canonical] = append(modelValue.scalars[property.Name.Canonical], scalar{member: property, primitive: primitive})
			} else if isBool(td) {
				modelValue.flags = append(modelValue.flags, property)
			} else if isURL(td) {
				modelValue.urls[property.Name.Canonical] = append(modelValue.urls[property.Name.Canonical], property)
			}
		}
	}
	ms.read[id] = modelValue
	return modelValue
}

// isBool reports whether td is the boolean primitive.
func isBool(td ir.TypeDef) bool {
	primitive, ok := td.(*ir.Primitive)
	return ok && primitive.PrimitiveKind == ir.PrimitiveBool
}

// wireNames lists the members' wire names, two or more, as listed does.
func wireNames(members []*ir.Property) string {
	names := make([]string, len(members))
	for i, property := range members {
		names[i] = strconv.Quote(property.EffectiveWireName())
	}
	return listed(names)
}
