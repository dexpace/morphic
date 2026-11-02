// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"encoding/json/jsontext"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// Links holds the links a service's responses declare until Resolve can read
// them: a link names an operation that may lower after the response carrying
// it, and a member of a type the registry may build only once every lowering
// has returned (docs/compilers/openapi.md#links). Its zero value holds none.
type Links struct {
	pending []pendingLink
}

// pendingLink is one drafted link and the response that carries it.
type pendingLink struct {
	from  ir.OpID
	slot  responseSlot
	draft linkDraft
}

// responseSlot names one response of an operation: the index-th of its
// Responses or Errors, or its Default. Each list is filled in source order and
// never reordered, so an index taken as a response is appended still names it
// once the service is whole.
type responseSlot struct {
	list  slotList
	index int
}

// slotList is the field of ir.Operation a responseSlot reads.
type slotList int

// The fields a response lowers to.
const (
	slotResponses slotList = iota
	slotErrors
	slotDefault
)

// add holds drafts, the links of the response slot names on from.
func (l *Links) add(from ir.OpID, slot responseSlot, drafts []linkDraft) {
	for i := range drafts {
		l.pending = append(l.pending, pendingLink{from: from, slot: slot, draft: drafts[i]})
	}
}

// linkDraft is a Link Object as its declaration reads, before what it names
// can be resolved.
type linkDraft struct {
	// at is the context of the response whose links map writes the entry, and
	// use the entry's pointer there, where a finding about it is reported: one
	// declaration shared by several responses can fail in one alone.
	at  lowering.Ctx
	use jsontext.Pointer
	// c is the context of the document declaring the link, and ptr its pointer
	// there, which its provenance and its server's ID name.
	c   lowering.Ctx
	ptr jsontext.Pointer

	name         string
	operationID  *string
	operationRef *string
	params       []draftParam
	body         *yaml.Node
	server       *soa.Server
	docs         ir.Docs
	extra        ir.Unmodeled // its extensions and undeclared keys
	node         *yaml.Node   // the link as it resolved, kept when it does not lower
}

// draftParam is one entry of a link's parameters map: a parameter name,
// qualified by its location or not, and the value as written.
type draftParam struct {
	key   string
	value *yaml.Node
}

// draftLinks drafts each link the response r, declared at rptr in c's
// document, names. A $ref the resolver could not follow is its finding, and
// drafts nothing.
func draftLinks(c lowering.Ctx, r *soa.Response, rptr jsontext.Pointer) ([]linkDraft, []ir.Diagnostic) {
	links := r.GetLinks()
	if links == nil {
		return nil, nil
	}
	var drafts []linkDraft
	var diags []ir.Diagnostic
	for name, rl := range links.All() {
		use := rptr + ids.Ptr("links", name)
		l, lc, lptr := lowering.Declared[soa.Link](c, rl, use)
		if l == nil {
			continue
		}
		d, draftDiags := draftLink(lc, l, lptr, name)
		d.at, d.use = c, use
		drafts = append(drafts, d)
		diags = append(diags, draftDiags...)
	}
	return drafts, diags
}

// draftLink drafts the link l named name, declared at ptr in c's document. It
// holds l whole, as it resolved, for keep.
func draftLink(c lowering.Ctx, l *soa.Link, ptr jsontext.Pointer, name string) (linkDraft, []ir.Diagnostic) {
	d := linkDraft{
		c: c, ptr: ptr, name: name,
		operationID: l.OperationID, operationRef: l.OperationRef,
		body: l.GetRequestBody(), server: l.GetServer(),
		docs: ir.Docs{Description: l.GetDescription()}, node: l.GetRootNode(),
	}
	if params := l.GetParameters(); params != nil {
		for key, value := range params.All() {
			d.params = append(d.params, draftParam{key: key, value: value})
		}
	}
	extra, diags := annotation.ExtensionsFrom(l.GetExtensions(), c.ProvenanceAt, ptr)
	d.extra = extra
	return d, append(diags, annotation.UnknownKeysIn(&d.extra, l, c.ProvenanceAt, ptr)...)
}

// keep records d verbatim in p, under openapi:links/<name>, reporting whether
// it wrote the entry and what preserving it reports. A link that does not
// lower is kept whole, as it resolved, so nothing of it is lost, a $ref'd one
// included; one that lowers is not converted at all, so nothing about its
// verbatim form is reported.
func (d *linkDraft) keep(p *ir.Unmodeled) (bool, []ir.Diagnostic) {
	return annotation.PreserveNodeInto(p, "openapi:"+ids.Scope("links", d.name),
		d.node, ir.ReasonNoIRHome, d.c.ProvenanceAt(d.ptr))
}
