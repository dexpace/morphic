// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// The pagination an extension declares, one target a field of ir.Pagination.
// A source listing no Values holds a runtime expression naming the value, as
// a link's parameters do; one listing Values marks the parameter, member or
// type it sits on as that value (docs/compilers/openapi.md#extension-pagination).
const (
	// TargetPaginationCursor is the input a cursor walk sends the next cursor
	// in, ir.Pagination.InputCursor of strategy cursor.
	TargetPaginationCursor ExtensionTarget = "pagination.cursor"
	// TargetPaginationOffset is the input an offset walk sends its item offset
	// in, InputCursor of strategy offset.
	TargetPaginationOffset ExtensionTarget = "pagination.offset"
	// TargetPaginationPage is the input a page walk sends its page number in,
	// InputCursor of strategy page.
	TargetPaginationPage ExtensionTarget = "pagination.page"
	// TargetPaginationLimit is the page-size input, InputLimit.
	TargetPaginationLimit ExtensionTarget = "pagination.limit"
	// TargetPaginationItems is where a page's items sit, Items.
	TargetPaginationItems ExtensionTarget = "pagination.items"
	// TargetPaginationNextCursor is the next cursor in a page, NextCursor.
	TargetPaginationNextCursor ExtensionTarget = "pagination.nextCursor"
	// TargetPaginationNextLink is the next page's link in a page, NextLink, a
	// walk of strategy next_link where no input continues it.
	TargetPaginationNextLink ExtensionTarget = "pagination.nextLink"
	// TargetPaginationTotalCount is the count of every item, TotalCount.
	TargetPaginationTotalCount ExtensionTarget = "pagination.totalCount"
	// TargetPaginationTotalPages is the count of every page, TotalPages.
	TargetPaginationTotalPages ExtensionTarget = "pagination.totalPages"
)

// paginationTargetPrefix starts the name of every pagination target.
const paginationTargetPrefix = "pagination."

// isPaginationTarget reports whether target names a field of ir.Pagination.
func isPaginationTarget(target ExtensionTarget) bool {
	return strings.HasPrefix(string(target), paginationTargetPrefix)
}

// PageStatement is what one source of a pagination target says on one node.
type PageStatement struct {
	// Expression is the runtime expression the source's value spells, empty
	// for a source that marks.
	Expression string
	// At is where the source's value is written, which a report names.
	At ir.Provenance
	// Stated reports whether the source says anything on the node: an
	// expression, or that the node is the value its target names.
	Stated bool
	// Written reports whether the source's key is on the node at all,
	// whatever its value: where it is, the document has spoken of the walk,
	// whether its value states a value, fails to or states nothing.
	Written bool
}

// Sources returns what the policy reads target from, in order of precedence.
// The list is the policy's own: a caller reads it and writes nothing to it.
func (p Promoter) Sources(target ExtensionTarget) []ExtensionSource {
	return p.targets[target]
}

// PageStatementOf reads what src, a source p reads the pagination target
// target from, says on the node keeping unmodeled
// (docs/compilers/openapi.md#extension-pagination): the expression its value
// spells where it lists no Values, and whether its value is one of them where
// it does. The key written false says nothing is there to read. Any other
// value stating nothing to every pagination source of the key is reported,
// worded alike for each of them, so one report stands per node.
func PageStatementOf(p Promoter, target ExtensionTarget, src ExtensionSource, unmodeled ir.Unmodeled) (PageStatement, []ir.Diagnostic) {
	entry, written := unmodeled[extensionKeyPrefix+src.Key]
	if !written {
		return PageStatement{}, nil
	}
	st := PageStatement{At: entry.Provenance, Written: true}
	raw, at, held, located := src.value(entry)
	switch {
	case located == states && listsWord(src, raw):
		st.At, st.Stated = at, true
		return st, nil
	case entry.Value.Kind() == 'f':
		return st, nil
	case located == misfit:
		return st, []ir.Diagnostic{unreadPage(src, src.Key, at,
			"is "+shapeOf(entry.Value)+", not an object", "value of a walk")}
	case located == statesNothing:
		return st, p.unreadMembers(src, held, at)
	case len(src.Values) > 0:
		return st, p.unlistedWord(src, raw, at)
	}
	text, ok := extensionText(raw)
	switch {
	case ok && text != "":
		st.At, st.Expression, st.Stated = at, text, true
		return st, nil
	case ok:
		return st, []ir.Diagnostic{unreadPage(src, src.String(), at, "is an empty string", string(target))}
	default:
		return st, []ir.Diagnostic{unreadPage(src, src.String(), at, "is "+shapeOf(raw)+", not a string",
			string(target))}
	}
}

// pageSources returns the sources of pagination targets p reads src's key from
// as src reads it: marking where src lists values and holding an expression
// where it lists none, and reading a member where src reads one. A value of
// the key is one statement to all of them, so what it states nothing to is
// judged across them.
func (p Promoter) pageSources(src ExtensionSource) []ExtensionSource {
	var out []ExtensionSource
	for target, sources := range p.targets {
		if !isPaginationTarget(target) {
			continue
		}
		for _, s := range sources {
			if s.Key == src.Key && (len(s.Values) > 0) == (len(src.Values) > 0) && (s.Member == "") == (src.Member == "") {
				out = append(out, s)
			}
		}
	}
	return out
}

// unreadMembers reports held, the members of src's key at at, where none of
// them is one a pagination source of the key reads.
func (p Promoter) unreadMembers(src ExtensionSource, held map[string]ir.RawValue, at ir.Provenance) []ir.Diagnostic {
	var read []string
	for _, s := range p.pageSources(src) {
		if _, ok := held[s.Member]; ok {
			return nil
		}
		read = append(read, s.Member)
	}
	return []ir.Diagnostic{unreadPage(src, src.Key, at,
		"holds none of the members the policy reads ("+joinSorted(read)+")", "value of a walk")}
}

// unlistedWord reports raw, at, a word src does not list, where no other
// pagination source of the same member lists it either.
func (p Promoter) unlistedWord(src ExtensionSource, raw ir.RawValue, at ir.Provenance) []ir.Diagnostic {
	var words []string
	for _, s := range p.pageSources(src) {
		if s.Member != src.Member {
			continue
		}
		if listsWord(s, raw) {
			return nil
		}
		words = append(words, s.Values...)
	}
	return []ir.Diagnostic{unreadPage(src, src.String(), at,
		"holds none of the words the policy lists ("+joinSorted(words)+")", "value of a walk")}
}

// unreadPage reports a value of src's key, at at, from which no value of the
// walk is read: subject spells the key or member holding it, why says what it
// is, and what names what it does not state. The key speaks of a walk wherever
// it is written, so the report says no pagination is inferred where it is
// read: for the operation an expression sits on, and each a mark's node is
// reached from.
func unreadPage(src ExtensionSource, subject string, at ir.Provenance, why, what string) ir.Diagnostic {
	reach := "the operation"
	if len(src.Values) > 0 {
		reach = "an operation reaching it"
	}
	return diag.Newf(ir.SeverityInfo, diag.DegradedConstruct, at,
		"extension %q %s, so it states no %s, and as the key is written, no pagination is inferred for %s",
		subject, why, what, reach)
}

// shapeOf names the kind of raw, a value in the IR's canonical form, as a
// report spells it.
func shapeOf(raw ir.RawValue) string {
	switch raw.Kind() {
	case 't':
		return "true"
	case 'f':
		return "false"
	case 'n':
		return "null"
	case '"':
		return "a string"
	case '0':
		return "a number"
	case '[':
		return "a list"
	default:
		return "an object"
	}
}

// joinSorted spells names for a report, sorted and each once.
func joinSorted(names []string) string {
	slices.Sort(names)
	return strings.Join(slices.Compact(names), ", ")
}
