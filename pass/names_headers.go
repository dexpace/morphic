// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// headers reads the headers of each response and error case as a scope of its
// own: those of a service's common errors, placed at the service, and those of
// an operation's responses, its default and its errors, placed at the
// operation. A target renders the headers of one response alike into one
// namespace, and no two responses share one. A part's headers wait for the way
// they are written (GitHub #1786). Operations past ir.ForEachOperation's bounds
// go unread, which validate reports as ir/walk-truncated.
func (r *nameReader) headers(doc *ir.Document) {
	for i := range doc.Services {
		svc := &doc.Services[i]
		for j := range svc.CommonErrors {
			e := &svc.CommonErrors[j]
			r.headerScope("service", string(svc.ID), e.Headers, func() string { return "common error " + nameLabel(e.Name) })
		}
	}
	ir.ForEachOperation(doc, func(op ir.Operation) {
		id := string(op.ID)
		for i := range op.Responses {
			resp := &op.Responses[i]
			r.headerScope("operation", id, resp.Headers, func() string { return "response " + nameLabel(resp.Name) })
		}
		if op.Default != nil {
			r.headerScope("operation", id, op.Default.Headers, func() string { return "the default response" })
		}
		for i := range op.Errors {
			e := &op.Errors[i]
			r.headerScope("operation", id, e.Headers, func() string { return "error " + nameLabel(e.Name) })
		}
	})
}

// headerScope reads headers, those of the part of in's id that of spells, as
// one scope. A header is named by its index, as a property is, beside its
// quoted spelling. of is called only for a finding.
func (r *nameReader) headerScope(in, id string, headers []ir.Header, of func() string) {
	if r.readScope(byWords, len(headers), func(i int) ir.Naming { return headers[i].Name }) {
		r.report(scope{in: in, id: id, of: of(), one: "header", many: "headers"})
	}
}

// nameLabel quotes the first of n's source, canonical and hint spellings that
// is set, and the empty string where none is.
func nameLabel(n ir.Naming) string {
	for _, s := range []string{n.Source, n.Canonical, n.Hint} {
		if s != "" {
			return strconv.Quote(s)
		}
	}
	return `""`
}
