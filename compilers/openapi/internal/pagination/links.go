// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// linkNames are the canonical names a page's link is read under
// (docs/compilers/openapi.md#pagination-policy): a member named one of named
// is the link where it is a string or a url, and, where none is, one named one
// of bare is where it is a url, as a plain string so named may be a cursor.
type linkNames struct {
	named, bare []string
}

// nextLinkNames are the names of a page's next link.
func nextLinkNames() linkNames {
	return linkNames{named: []string{"next_link"}, bare: []string{"next"}}
}

// prevLinkNames are the names of a page's previous link.
func prevLinkNames() linkNames {
	return linkNames{named: []string{"prev_link", "previous_link"}, bare: []string{"prev", "previous"}}
}

// link returns the path to the one member of body, or, where deep is set, of a
// model one of its members holds, that names is a link under, and nil where
// none or several are.
func (r *reader) link(body *model, deep bool, names linkNames) []ir.PropID {
	path, found := r.findLink(body, deep, names.named, true)
	if found == 0 {
		path, found = r.findLink(body, deep, names.bare, false)
	}
	if found != 1 {
		return nil
	}
	return path
}

// findLink returns how many members of body, and, where deep is set, of a
// model one of its members holds, are named one of names and typed as a url,
// or as a plain string where plain is set, and the path to one of them.
func (r *reader) findLink(body *model, deep bool, names []string, plain bool) ([]ir.PropID, int) {
	var path []ir.PropID
	found := 0
	at := func(m *model, through []ir.PropID) {
		for _, name := range names {
			for _, p := range m.urls[name] {
				path, found = append(slices.Clone(through), p.ID), found+1
			}
			for _, s := range m.scalars[name] {
				if plain && s.prim == ir.PrimString {
					path, found = append(slices.Clone(through), s.member.ID), found+1
				}
			}
		}
	}
	at(body, nil)
	if deep {
		for _, o := range body.objects {
			at(r.model(o.id), []ir.PropID{o.member.ID})
		}
	}
	return path, found
}

// agreed returns what of reads off every page of pages, where each reads the
// same, and nil where two differ: a pager reads whichever content arrives, so
// a link one content holds apart from another is no link of the walk.
func agreed(pages []page, of func(page) []ir.PropID) []ir.PropID {
	first := of(pages[0])
	for _, pg := range pages[1:] {
		if !slices.Equal(first, of(pg)) {
			return nil
		}
	}
	return first
}

// headerCursor returns the response header carrying the next cursor of a walk
// sending cursor, a canonical name, and its wire name: the one header every
// success response of op declares once (headerOutput) whose canonical name, a
// leading x_ aside, is next_<cursor>, else the cursor's own. It returns nil
// where none is.
func headerCursor(w *whole.Document, op *ir.Operation, cursor string) (*ir.PropPath, string) {
	if len(op.Responses) == 0 {
		return nil, ""
	}
	for _, name := range [...]string{"next_" + cursor, cursor} {
		var wire []string
		for i := range op.Responses[0].Headers {
			h := &op.Responses[0].Headers[i].Property
			if strings.TrimPrefix(h.Name.Canonical, "x_") == name {
				wire = append(wire, h.EffectiveWireName())
			}
		}
		if len(wire) != 1 {
			continue
		}
		if out, failure := headerOutput(w, op, wire[0]); failure == "" {
			return out, wire[0]
		}
	}
	return nil, ""
}

// isURL reports whether td is the url primitive.
func isURL(td ir.TypeDef) bool {
	p, ok := td.(*ir.Primitive)
	return ok && p.Prim == ir.PrimURL
}

// linkWalk returns the walk op's response alone continues, where no place
// names a continuation (docs/compilers/openapi.md#pagination-policy): a
// next_link walk where a GET's or a QUERY's every page content is a page
// (reader.linkPage) stating a next link, and a link_header walk where each is
// a page or a list stating none and every success response with a body
// declares a Link header. A response that is no page triggers nothing and
// draws nothing. why says what stops it where the contents hold their pages
// apart or state two continuations.
func (r *reader) linkWalk(op *ir.Operation) (*ir.Pagination, string) {
	if _, kind := methodOf(op); kind != methodSafe {
		return nil, ""
	}
	var pages []page
	for ct := range pageContents(op) {
		pg := r.page(pageKey{body: ct.Type.Target, link: true})
		if pg.why != "" {
			return nil, ""
		}
		pages = append(pages, pg)
	}
	if len(pages) == 0 {
		return nil, ""
	}
	linked := slices.ContainsFunc(pages, func(pg page) bool { return pg.nextLink != nil })
	header := linkHeader(r.w, op)
	switch {
	case !linked && !header:
		return nil, ""
	case slices.ContainsFunc(pages[1:], func(pg page) bool { return !sameLinkPage(pages[0], pg) }):
		return nil, "the success responses' bodies hold their pages at different members"
	case linked && header:
		return nil, "the response states a next link in its body and in a Link header, two continuations"
	}
	strategy := ir.PageStrategyLinkHeader
	if linked {
		strategy = ir.PageStrategyNextLink
	}
	return &ir.Pagination{
		Strategy:   strategy,
		Inferred:   true,
		Items:      &ir.PropPath{Segments: slices.Clone(pages[0].items)},
		NextLink:   pathTo(pages[0].nextLink),
		PrevLink:   pathTo(pages[0].prevLink),
		TotalCount: pathTo(pages[0].totalCount),
		TotalPages: pathTo(pages[0].totalPages),
		Stop:       []ir.PageStop{ir.PageStopNoNext},
	}, ""
}

// sameLinkPage reports whether p and q hold a link walk's page, links
// included, at the same members.
func sameLinkPage(p, q page) bool {
	return p.equal(q) && slices.Equal(p.nextLink, q.nextLink) && slices.Equal(p.prevLink, q.prevLink)
}

// linkPart is what a member of a link walk's page is.
type linkPart int

// The parts of a link walk's page.
const (
	// partOther is a member no page of a link walk holds.
	partOther linkPart = iota
	// partItems is the list of the page's items.
	partItems
	// partNext is the next link.
	partNext
	// partPrev is the previous link.
	partPrev
	// partCount is a count of the items or the pages.
	partCount
)

// linkPartOf returns what a member named canonical, of the type td, is in a
// link walk's page: its items, a list named as items or value, the name the
// next-link convention pairs with nextLink; a link, as linkNames name one,
// at the root; or a whole-number count named count, total, total_count or
// total_pages.
func linkPartOf(canonical string, td ir.TypeDef) linkPart {
	prim, _ := td.(*ir.Primitive)
	text := prim != nil && (prim.Prim == ir.PrimString || prim.Prim == ir.PrimURL)
	url := prim != nil && prim.Prim == ir.PrimURL
	switch canonical {
	case "next_link":
		return partIf(text, partNext)
	case "next":
		return partIf(url, partNext)
	case "prev_link", "previous_link":
		return partIf(text, partPrev)
	case "prev", "previous":
		return partIf(url, partPrev)
	case "count", "total", "total_count", "total_pages":
		return partIf(prim != nil && (prim.Prim == ir.PrimInteger || prim.Prim.StrictlyWithin(ir.PrimInteger)), partCount)
	}
	if _, list := td.(*ir.List); list && (itemsName(canonical) || canonical == "value") {
		return partItems
	}
	return partOther
}

// partIf is part where ok holds, and partOther otherwise.
func partIf(ok bool, part linkPart) linkPart {
	if ok {
		return part
	}
	return partOther
}

// linkPage reads the model id as a link walk's page: one list of items, the
// links and counts, each at its root, and no other member, a resource that
// links to its neighbour, a title or an id beside its links, being none. why
// is set where it is no page.
func (r *reader) linkPage(id ir.TypeID) page {
	parts := map[linkPart][]*ir.Property{}
	for _, p := range r.w.Members(id) {
		part := linkPartOf(p.Name.Canonical, r.w.Stands(p.Type.Target))
		if part == partOther {
			return page{why: "is no page"}
		}
		parts[part] = append(parts[part], p)
	}
	if len(parts[partItems]) != 1 {
		return page{why: "is no page"}
	}
	body := r.model(id)
	return page{items: []ir.PropID{parts[partItems][0].ID}, nextLink: r.link(body, false, nextLinkNames()),
		prevLink: r.link(body, false, prevLinkNames()), totalCount: r.total(body, "total_count"),
		totalPages: r.total(body, "total_pages")}
}

// linkHeader reports whether every success response of op declaring a page
// body declares one Link header of a plain string type, the header RFC 8288
// names, and one does.
func linkHeader(w *whole.Document, op *ir.Operation) bool {
	declared := false
	for i := range op.Responses {
		if pageBody(op.Responses[i].Payload) == nil {
			continue
		}
		headers := op.Responses[i].Headers
		found := w.Header(whole.ResponseKey{Op: op.ID, List: whole.ListResponses, Index: i}, headers, "Link")
		if len(found) != 1 {
			return false
		}
		at := slices.IndexFunc(headers, func(h ir.Header) bool { return h.ID == found[0] })
		if prim, ok := w.Stands(headers[at].Type.Target).(*ir.Primitive); !ok || prim.Prim != ir.PrimString {
			return false
		}
		declared = true
	}
	return declared
}
