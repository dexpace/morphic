// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pagination

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
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

// pageLink returns the path to the one member of body, or, where deep is set, of a
// model one of its members holds, that names is a link under, and nil where
// none or several are.
func pageLink(ms *models, body *model, deep bool, names linkNames) []ir.PropertyID {
	path, found := findLink(ms, body, deep, names.named, true)
	if found == 0 {
		path, found = findLink(ms, body, deep, names.bare, false)
	}
	if found != 1 {
		return nil
	}
	return path
}

// findLink returns how many members of body, and, where deep is set, of a
// model one of its members holds, are named one of names and typed as a url,
// or as a plain string where plain is set, and the path to one of them.
func findLink(ms *models, body *model, deep bool, names []string, plain bool) ([]ir.PropertyID, int) {
	var path []ir.PropertyID
	found := 0
	at := func(modelValue *model, through []ir.PropertyID) {
		for _, name := range names {
			for _, property := range modelValue.urls[name] {
				path, found = append(slices.Clone(through), property.ID), found+1
			}
			for _, scalar := range modelValue.scalars[name] {
				if plain && scalar.primitive == ir.PrimitiveString {
					path, found = append(slices.Clone(through), scalar.member.ID), found+1
				}
			}
		}
	}
	at(body, nil)
	if deep {
		for _, object := range body.objects {
			at(ms.of(object.id), []ir.PropertyID{object.member.ID})
		}
	}
	return path, found
}

// agreed returns what of reads off every page of pages, where each reads the
// same, and nil where two differ: a pager reads whichever content arrives, so
// a link one content holds apart from another is no link of the walk.
func agreed(pages []page, of func(page) []ir.PropertyID) []ir.PropertyID {
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
// success response of operation declares once (headerOutput) whose canonical
// name, a leading x_ aside, is next_<cursor>, else the cursor's own, and the
// least of its spellings. It returns nil where none is, or where two headers,
// as HTTP compares their names, are so named.
func headerCursor(compiledDocumentView *compileddoc.CompiledDocumentView, operation *ir.Operation, cursor string) (*ir.PropertyPath, string) {
	for _, name := range [...]string{"next_" + cursor, cursor} {
		spellings := map[string][]string{} // by the name as HTTP compares it
		for i := range operation.Responses {
			for j := range operation.Responses[i].Headers {
				property := &operation.Responses[i].Headers[j].Property
				if wire := property.EffectiveWireName(); strings.TrimPrefix(property.Name.Canonical, "x_") == name {
					spellings[strings.ToLower(wire)] = append(spellings[strings.ToLower(wire)], wire)
				}
			}
		}
		if len(spellings) != 1 {
			continue
		}
		for _, spelled := range spellings {
			least := slices.Min(spelled)
			if out, failure := headerOutput(compiledDocumentView, operation, least); failure == "" {
				return out, least
			}
		}
	}
	return nil, ""
}

// isURL reports whether td is the url primitive.
func isURL(td ir.TypeDef) bool {
	primitive, ok := td.(*ir.Primitive)
	return ok && primitive.PrimitiveKind == ir.PrimitiveURL
}

// linkWalk returns the walk operation's response alone continues, where no
// place names a continuation (docs/compilers/openapi.md#pagination-policy): a
// next_link walk where a GET's or a QUERY's every page content is a page
// (reader.linkPage) stating a next link, and a link_header walk where each is a
// page or a list stating none and every success response with a body declares a
// Link header. A response that is no page triggers nothing and draws nothing.
// why says what stops it where the contents hold their pages apart or state two
// continuations.
func (r *reader) linkWalk(operation *ir.Operation) (*ir.Pagination, string) {
	if _, kind := methodOf(operation); kind != methodSafe {
		return nil, ""
	}
	var pages []page
	for ct := range pageContents(operation) {
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
	header := linkHeader(r.w, operation)
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
		Items:      &ir.PropertyPath{Segments: slices.Clone(pages[0].items)},
		NextLink:   pathTo(pages[0].nextLink),
		PrevLink:   pathTo(pages[0].prevLink),
		TotalCount: pathTo(pages[0].totalCount),
		TotalPages: pathTo(pages[0].totalPages),
		Stop:       []ir.PageStop{ir.PageStopNoNext},
	}, ""
}

// sameLinkPage reports whether pageValue and otherPage hold a link walk's page,
// links included, at the same members.
func sameLinkPage(pageValue, otherPage page) bool {
	return pageValue.equal(otherPage) && slices.Equal(pageValue.nextLink, otherPage.nextLink) && slices.Equal(pageValue.prevLink, otherPage.prevLink)
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
	primitive, _ := td.(*ir.Primitive)
	text := primitive != nil && (primitive.PrimitiveKind == ir.PrimitiveString || primitive.PrimitiveKind == ir.PrimitiveURL)
	url := primitive != nil && primitive.PrimitiveKind == ir.PrimitiveURL
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
		return partIf(primitive != nil && (primitive.PrimitiveKind == ir.PrimitiveInteger || primitive.PrimitiveKind.StrictlyWithin(ir.PrimitiveInteger)), partCount)
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
	for _, property := range r.w.Members(id) {
		part := linkPartOf(property.Name.Canonical, r.w.Stands(property.Type.Target))
		if part == partOther {
			return page{why: "is no page"}
		}
		parts[part] = append(parts[part], property)
	}
	if len(parts[partItems]) != 1 {
		return page{why: "is no page"}
	}
	body := r.models.of(id)
	return page{items: []ir.PropertyID{parts[partItems][0].ID}, nextLink: pageLink(r.models, body, false, nextLinkNames()),
		prevLink: pageLink(r.models, body, false, prevLinkNames()), totalCount: pageTotal(r.models, body, "total_count"),
		totalPages: pageTotal(r.models, body, "total_pages")}
}

// linkHeader reports whether every success response of operation declaring a
// page body declares one Link header of a plain string type, the header RFC
// 8288 names, and one does.
func linkHeader(compiledDocumentView *compileddoc.CompiledDocumentView, operation *ir.Operation) bool {
	declared := false
	for i := range operation.Responses {
		if pageBody(operation.Responses[i].Payload) == nil {
			continue
		}
		headers := operation.Responses[i].Headers
		found := compiledDocumentView.Header(compileddoc.ResponseKey{Operation: operation.ID, List: compileddoc.ListResponses, Index: i}, headers, "Link")
		if len(found) != 1 {
			return false
		}
		at := slices.IndexFunc(headers, func(header ir.Header) bool { return header.ID == found[0] })
		if primitive, ok := compiledDocumentView.Stands(headers[at].Type.Target).(*ir.Primitive); !ok || primitive.PrimitiveKind != ir.PrimitiveString {
			return false
		}
		declared = true
	}
	return declared
}
