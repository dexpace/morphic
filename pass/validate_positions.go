// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// position is a place where a type, bounds and a value meet: a property, a
// parameter or a header. Each check of what a source says about a value reads
// all of them alike, so they are read through one list.
type position struct {
	// node is the ID a finding at the position is placed at.
	node string
	// from is the provenance of the position's entity.
	from ir.Provenance
	typ  ir.TypeRef
	own  *ir.Constraints
	// encoded says the value is written in an encoding of its own, so its kind
	// says nothing of what the type holds.
	encoded  bool
	def      *ir.Value
	examples []ir.Example
}

// eachPosition calls visit with each property of a live model, in liveTypeIDs
// order, then each header of a service's common errors, then each parameter and
// header of each operation as ir.ForEachOperation visits them: its parameters,
// the headers of its responses, its default and its errors, and last the
// headers of a content's parts, the parts in sorted order, read through keys.
// visit must not keep its argument.
//
// Not read: Property.Args, which is GraphQL's, and a resource's properties,
// which no compiler writes (#1413). A stop of ir.ForEachOperation is the one
// Validate reports for each check that visits operations.
func eachPosition(doc *ir.Document, live []ir.TypeID, keys *[]ir.PropertyID, visit func(*position)) {
	var p position
	for _, id := range live {
		m, ok := doc.Types[id].(*ir.Model)
		if !ok {
			continue
		}
		for i := range m.Properties {
			visit(propertyPosition(&p, &m.Properties[i], string(id)))
		}
	}
	for i := range doc.Services {
		for j := range doc.Services[i].CommonErrors {
			headerPositions(&p, doc.Services[i].CommonErrors[j].Headers, string(doc.Services[i].ID), visit)
		}
	}
	ir.ForEachOperation(doc, func(op ir.Operation) { operationPositions(&p, &op, keys, visit) })
}

// propertyPosition fills p from pr, a property of owner, which stands in for
// the property's own ID where that is empty.
func propertyPosition(p *position, pr *ir.Property, owner string) *position {
	node := string(pr.ID)
	if node == "" {
		node = owner
	}
	*p = position{
		node: node, from: pr.Provenance, typ: pr.Type, own: pr.Constraints,
		encoded: pr.Encoding != nil, def: pr.Default, examples: pr.Examples,
	}
	return p
}

// headerPositions visits each of headers, which belong to owner.
func headerPositions(p *position, headers []ir.Header, owner string, visit func(*position)) {
	for i := range headers {
		visit(propertyPosition(p, &headers[i].Property, owner))
	}
}

// operationPositions visits the positions of op.
func operationPositions(p *position, op *ir.Operation, keys *[]ir.PropertyID, visit func(*position)) {
	for i := range op.Params {
		pm := &op.Params[i]
		*p = position{
			node: string(pm.ID), from: pm.Provenance, typ: pm.Type, own: pm.Constraints,
			def: pm.Default, examples: pm.Examples,
		}
		visit(p)
	}
	owner := string(op.ID)
	for i := range op.Responses {
		headerPositions(p, op.Responses[i].Headers, owner, visit)
	}
	if op.Default != nil {
		headerPositions(p, op.Default.Headers, owner, visit)
	}
	for i := range op.Errors {
		headerPositions(p, op.Errors[i].Headers, owner, visit)
	}
	partPositions(p, op.Request, owner, keys, visit)
	for i := range op.Responses {
		partPositions(p, op.Responses[i].Payload, owner, keys, visit)
	}
	if op.Default != nil {
		partPositions(p, op.Default.Payload, owner, keys, visit)
	}
	for i := range op.Errors {
		partPositions(p, op.Errors[i].Payload, owner, keys, visit)
	}
}

// partPositions visits the headers of the parts of each content of payload: the
// item's, then each part's, in the order of its sorted keys.
func partPositions(p *position, payload *ir.Payload, owner string, keys *[]ir.PropertyID, visit func(*position)) {
	if payload == nil {
		return
	}
	for i := range payload.Contents {
		c := &payload.Contents[i]
		if c.ItemEncoding != nil {
			headerPositions(p, c.ItemEncoding.Headers, owner, visit)
		}
		*keys = (*keys)[:0]
		for k := range c.Encoding {
			*keys = append(*keys, k)
		}
		slices.Sort(*keys)
		for _, k := range *keys {
			part := c.Encoding[k]
			headerPositions(p, part.Headers, owner, visit)
		}
	}
}
