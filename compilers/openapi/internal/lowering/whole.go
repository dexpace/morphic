// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"strings"

	"github.com/dexpace/morphic/ir"
)

// MaxAliasHops bounds how many alias scalars Stands reads a type through
// (styleguide bounded-everything rule), as the schema walk bounds a tag's. A
// longer chain, or a loop, stands for no type a step reads.
const MaxAliasHops = 64

// ResponseList is the field of an ir.Operation a response sits in.
type ResponseList int

// The fields an operation holds its responses in.
const (
	ListResponses ResponseList = iota
	ListErrors
	ListDefault
)

// ResponseKey names one response of a service, which Whole indexes the headers
// of: the operation carrying it, the list it sits in, and where in the list.
// The default response is the one of ListDefault, at Index 0.
type ResponseKey struct {
	Op    ir.OpID
	List  ResponseList
	Index int
}

// Whole is a finished document as the steps after the lowerings read it: its
// links, promotions and inferences (docs/compilers/openapi.md#policies). It
// lists the operations once, and indexes what those steps read again and
// again, each the first time a step reads it: a model's members by wire name,
// an operation's parameters and a response's headers. A step reading the
// document reads it here, so none walks or indexes it for itself, and a
// member is read through alias scalars by every step alike.
type Whole struct {
	// Types is the document's registry, which is whole.
	Types ir.TypeRegistry
	// Ops is every operation of every service, those of nested groups included
	// to ir.MaxGroupDepth, as pointers into the document, a service's before
	// the next's.
	Ops []*ir.Operation
	// Auth is the document's security schemes, the map the document holds.
	Auth map[ir.AuthID]ir.AuthScheme

	names   *ir.MemberReader
	taken   map[memberKey]*ir.Property
	members map[ir.TypeID][]*ir.Property
	params  map[ir.OpID]ParamIndex
	headers map[ResponseKey]map[string][]ir.PropID
	// flatten is ir.FlattenedProperties, which this package's tests count
	// through.
	flatten func(ir.TypeRegistry, ir.TypeID) ir.Flattening
}

// memberKey is one member Member is asked for: a type, and a wire name.
type memberKey struct {
	id   ir.TypeID
	wire string
}

// NewWhole returns a Whole over doc, whose registry and service tree are
// whole, that has indexed nothing yet.
func NewWhole(doc *ir.Document) *Whole {
	w := &Whole{
		Types:   doc.Types,
		Auth:    doc.Auth,
		names:   ir.NewMemberReader(doc.Types),
		taken:   map[memberKey]*ir.Property{},
		members: map[ir.TypeID][]*ir.Property{},
		params:  map[ir.OpID]ParamIndex{},
		headers: map[ResponseKey]map[string][]ir.PropID{},
		flatten: ir.FlattenedProperties,
	}
	for i := range doc.Services {
		w.Ops = append(w.Ops, operations(&doc.Services[i])...)
	}
	return w
}

// Stands returns the type id stands for, read through at most MaxAliasHops
// alias scalars, and nil past them or where the registry holds none, a typed
// nil included, so a caller's comma-ok assertion holds a node.
func (w *Whole) Stands(id ir.TypeID) ir.TypeDef {
	for range MaxAliasHops {
		base, alias := ir.AliasBase(w.Types, id)
		if !alias {
			break
		}
		id = base
	}
	td := w.Types[id]
	if _, alias := ir.AliasBase(w.Types, id); alias || ir.IsNilTypeDef(td) {
		return nil
	}
	return td
}

// Member returns the declaration the flattening of id takes for the wire name
// wire (ir.MemberReader), id read through alias scalars, and false where id
// names no model or the model holds no such member. Each answer is read once.
func (w *Whole) Member(id ir.TypeID, wire string) (*ir.Property, bool) {
	key := memberKey{id: id, wire: wire}
	if p, read := w.taken[key]; read {
		return p, p != nil
	}
	var p *ir.Property
	if decl, ok := w.names.Member(id, wire); ok {
		p = decl.Property
	}
	w.taken[key] = p
	return p, p != nil
}

// Walk returns the property each token names, from the model root through the
// type of each member it reaches, and false where a step reaches no model, or
// a model declaring no member of that name.
func (w *Whole) Walk(root ir.TypeID, tokens []string) ([]ir.PropID, bool) {
	segments := make([]ir.PropID, 0, len(tokens))
	at := root
	for _, token := range tokens {
		member, ok := w.Member(at, token)
		if !ok {
			return nil, false
		}
		segments = append(segments, member.ID)
		at = member.Type.Target
	}
	return segments, true
}

// Members returns the members of the model id names, read through alias
// scalars, in layout order (ir.FlattenedProperties), and none for any other
// type. It flattens each model once, for a step that reads every member; one
// asking for a name reads it with Member, which flattens nothing.
func (w *Whole) Members(id ir.TypeID) []*ir.Property {
	if props, read := w.members[id]; read {
		return props
	}
	var props []*ir.Property
	if flat := w.flatten(w.Types, id); len(flat.Properties) > 0 {
		props = make([]*ir.Property, len(flat.Properties))
		for i := range flat.Properties {
			props[i] = flat.Properties[i].Property
		}
	}
	w.members[id] = props
	return props
}

// Params returns op's parameters by where its HTTP bindings place them,
// indexed at the first ask and kept.
func (w *Whole) Params(op *ir.Operation) ParamIndex {
	ix, indexed := w.params[op.ID]
	if !indexed {
		ix = indexParams(op)
		w.params[op.ID] = ix
	}
	return ix
}

// Header returns the headers among headers that declare name, its case aside,
// as HTTP compares header names. headers are those of the response at names:
// the first read of a response indexes them and a later read takes that index,
// so reads of one key pass the same headers.
func (w *Whole) Header(at ResponseKey, headers []ir.Header, name string) []ir.PropID {
	byName, indexed := w.headers[at]
	if !indexed {
		byName = make(map[string][]ir.PropID, len(headers))
		for i := range headers {
			p := &headers[i].Property
			key := strings.ToLower(p.EffectiveWireName())
			byName[key] = append(byName[key], p.ID)
		}
		w.headers[at] = byName
	}
	return byName[strings.ToLower(name)]
}
