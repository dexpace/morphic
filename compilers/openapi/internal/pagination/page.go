// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// maxAliasHops bounds how many alias scalars a member's or a body's type is
// read through (styleguide bounded-everything rule), as the schema walk bounds
// a tag's. A longer chain, or a loop, stands for no type a walk reads.
const maxAliasHops = 64

// reader reads what a walk needs off a document's registry, which must be
// whole: what a model's members are, and where a body holds a page, each read
// once however many operations or members name it.
type reader struct {
	types  ir.TypeRegistry
	models map[ir.TypeID]*model
	pages  map[pageKey]page
}

// newReader returns a reader over types that has read nothing yet.
func newReader(types ir.TypeRegistry) *reader {
	return &reader{types: types, models: map[ir.TypeID]*model{}, pages: map[pageKey]page{}}
}

// model is what a page reads of one model's members across its composition
// (ir.FlattenedProperties), each as the declaration its flattening takes,
// which is the type a page decodes it as: those that are lists, those that are
// models, with the model, and those of a type a walk sends, by canonical name.
type model struct {
	lists   []*ir.Property
	objects []object
	scalars map[string][]scalar
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

// pageKey is a body read for a walk: its type, and for a walk reading a next
// cursor the canonical name and the primitive of the parameter it feeds, which
// name and type the member carrying it.
type pageKey struct {
	body   ir.TypeID
	cursor string
	prim   ir.PrimKind
}

// page is where a body holds what a walk reads, each a path of members from
// the body's root: its items, none for the body whole, and its next cursor and
// totals, nil where it states none. why says, where it is set, what the body
// is or holds that leaves it no page, and the paths are then unset.
type page struct {
	items, next, totalCount, totalPages []ir.PropID
	why                                 string
}

// equal reports whether p and q, two pages a body holds, hold them at the same
// members.
func (p page) equal(q page) bool {
	return slices.Equal(p.items, q.items) && slices.Equal(p.next, q.next) &&
		slices.Equal(p.totalCount, q.totalCount) && slices.Equal(p.totalPages, q.totalPages)
}

// responsePage returns where op's success responses hold the page a walk
// sending in reads. Every content of every one must hold it at the same
// members, since a pager reads whichever arrives; one with no body, a 204 or a
// 304, holds none, as a pager ends at it as at an empty page
// (docs/emitters/plan/responses.md#other-responses). why says otherwise what
// stops it.
func (r *reader) responsePage(op *ir.Operation, in inputs) (page, string) {
	if len(op.Responses) == 0 {
		return page{}, "the operation declares no success response to read a page from"
	}
	key := pageKey{}
	if in.readsNextCursor() {
		key.cursor, key.prim = in.cursor.Name.Canonical, in.prim
	}
	var first *page
	for i := range op.Responses {
		p := op.Responses[i].Payload
		if p == nil {
			continue
		}
		for j := range p.Contents {
			ct := &p.Contents[j]
			key.body = ct.Type.Target
			pg := r.page(key)
			switch {
			case pg.why != "":
				return page{}, fmt.Sprintf("the %s response body %s", ct.MediaType, pg.why)
			case first == nil:
				first = &pg
			case !first.equal(pg):
				return page{}, "the success responses' bodies hold their pages at different members"
			}
		}
	}
	if first == nil {
		return page{}, "no success response has a body to read a page from"
	}
	return *first, ""
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
	switch td := resolve(r.types, key.body).(type) {
	case *ir.List:
		if key.cursor != "" {
			return page{why: "is a list, which holds no next cursor"}
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
	items, why := r.items(body)
	if why != "" {
		return page{why: why}
	}
	pg := page{items: items}
	if key.cursor != "" {
		next, notNext := r.nextCursor(body, key)
		if notNext != "" {
			return page{why: notNext}
		}
		pg.next = next
	}
	pg.totalCount = r.total(body, "total_count")
	pg.totalPages = r.total(body, "total_pages")
	return pg
}

// nextCursor returns the path to the one member of members, or of a model one
// of them holds, of the cursor's primitive and named as the cursor with next
// before it, or, where none is, as the cursor itself: a page naming both
// echoes the cursor it was sent beside the next one.
func (r *reader) nextCursor(body *model, key pageKey) ([]ir.PropID, string) {
	names := [...]string{"next_" + key.cursor, key.cursor}
	for _, name := range names {
		path, found := r.find(body, name, func(prim ir.PrimKind) bool { return prim == key.prim })
		switch found {
		case 0:
			continue
		case 1:
			return path, ""
		default:
			return nil, fmt.Sprintf("holds %d members %q of the cursor's type, each a next cursor", found, name)
		}
	}
	return nil, fmt.Sprintf("holds no member %q or %q of the cursor's type", names[0], names[1])
}

// total returns the path to the one member named name, of a type a walk counts
// with, or nil where the body holds none or several.
func (r *reader) total(body *model, name string) []ir.PropID {
	path, found := r.find(body, name, func(ir.PrimKind) bool { return true })
	if found != 1 {
		return nil
	}
	return path
}

// find returns how many members named name, of body or of a model one of its
// members holds, are of a primitive a walk sends that admit accepts, and the
// path to one of them, which is the only one when that is one.
func (r *reader) find(body *model, name string, admit func(ir.PrimKind) bool) ([]ir.PropID, int) {
	var path []ir.PropID
	found := 0
	for _, s := range body.scalars[name] {
		if admit(s.prim) {
			path, found = []ir.PropID{s.member.ID}, found+1
		}
	}
	for _, o := range body.objects {
		for _, s := range r.model(o.id).scalars[name] {
			if admit(s.prim) {
				path, found = []ir.PropID{o.member.ID, s.member.ID}, found+1
			}
		}
	}
	return path, found
}

// items returns the path to the list body holds a page's items in: the one
// list named as items; else the one list the one model named so holds; else
// the body's one list. why says otherwise what stops it.
func (r *reader) items(body *model) ([]ir.PropID, string) {
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
	m := &model{scalars: map[string][]scalar{}}
	for _, fp := range ir.FlattenedProperties(r.types, id).Properties {
		p := fp.Property
		switch td := resolve(r.types, p.Type.Target).(type) {
		case *ir.List:
			m.lists = append(m.lists, p)
		case *ir.Model:
			m.objects = append(m.objects, object{member: p, id: td.ID})
		default:
			if prim, ok := sendableAs(td); ok {
				m.scalars[p.Name.Canonical] = append(m.scalars[p.Name.Canonical], scalar{member: p, prim: prim})
			}
		}
	}
	r.models[id] = m
	return m
}

// resolve returns the type id stands for, read through at most maxAliasHops
// alias scalars, and nil past them or where types holds none, a typed nil
// included, so a caller's comma-ok assertion holds a node.
func resolve(types ir.TypeRegistry, id ir.TypeID) ir.TypeDef {
	for range maxAliasHops {
		base, alias := ir.AliasBase(types, id)
		if !alias {
			break
		}
		id = base
	}
	td := types[id]
	if _, alias := ir.AliasBase(types, id); alias || ir.IsNilTypeDef(td) {
		return nil
	}
	return td
}

// wireNames lists the members' wire names, two or more, as listed does.
func wireNames(members []*ir.Property) string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = strconv.Quote(m.EffectiveWireName())
	}
	return listed(names)
}
