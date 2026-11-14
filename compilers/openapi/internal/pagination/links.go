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
