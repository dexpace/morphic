// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compileddoc

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

// ResponseKey names one response of a service, which Document indexes the headers
// of: the operation carrying it, the list it sits in, and where in the list.
// The default response is the one of ListDefault, at Index 0.
type ResponseKey struct {
	Operation ir.OperationID
	List      ResponseList
	Index     int
}

// CompiledDocumentView is a finished document as the steps after the lowerings
// read it: its links, promotions and inferences
// (docs/compilers/openapi.md#policies). It lists the operations once, and
// indexes what those steps read again and again, each at the first ask: a
// model's members by wire name, an operation's parameters and a response's
// headers. An index is kept, so no step may append a property, parameter,
// binding or header after one. New runs before the document's root is lowered,
// so a step reading the root's Unmodeled takes it as a parameter of its own.
type CompiledDocumentView struct {
	// Types is the document's registry, which is whole.
	Types ir.TypeRegistry
	// Operations is every operation of every service, those of nested groups included
	// to ir.MaxGroupDepth, as pointers into the document, a service's before
	// the next's.
	Operations []*ir.Operation
	// Auth is the document's security schemes, the map the document holds.
	Auth map[ir.AuthID]ir.AuthScheme

	names   *ir.MemberReader
	taken   map[memberKey]*ir.Property
	members map[ir.TypeID][]*ir.Property
	params  map[ir.OperationID]ParamIndex
	headers map[ResponseKey]map[string][]ir.PropertyID
	// flatten is ir.FlattenedProperties, which this package's tests count
	// through.
	flatten func(ir.TypeRegistry, ir.TypeID) ir.Flattening
}

// memberKey is one member Member is asked for: a type, and a wire name.
type memberKey struct {
	id   ir.TypeID
	wire string
}

// New returns a Document over doc, whose registry and service tree are whole,
// that has indexed nothing yet.
func New(doc *ir.Document) *CompiledDocumentView {
	compiledDocumentView := &CompiledDocumentView{
		Types:   doc.Types,
		Auth:    doc.Auth,
		names:   ir.NewMemberReader(doc.Types),
		taken:   map[memberKey]*ir.Property{},
		members: map[ir.TypeID][]*ir.Property{},
		params:  map[ir.OperationID]ParamIndex{},
		headers: map[ResponseKey]map[string][]ir.PropertyID{},
		flatten: ir.FlattenedProperties,
	}
	for i := range doc.Services {
		compiledDocumentView.Operations = append(compiledDocumentView.Operations, operations(&doc.Services[i])...)
	}
	return compiledDocumentView
}

// Stands returns the type id stands for, read through at most MaxAliasHops
// alias scalars, and nil past them or where the registry holds none, a typed
// nil included, so a caller's comma-ok assertion holds a node.
func (w *CompiledDocumentView) Stands(id ir.TypeID) ir.TypeDef {
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

// Member returns the declaration the flattening of the model id stands for
// (Stands) takes for the wire name wire (ir.MemberReader), and false where id
// stands for no model or the model holds no such member, so it reads a chain
// of alias scalars as Members does. Each answer is read once, by a walk of
// the model's composition, so a model read for n names costs n walks (#1955).
func (w *CompiledDocumentView) Member(id ir.TypeID, wire string) (*ir.Property, bool) {
	key := memberKey{id: id, wire: wire}
	if property, read := w.taken[key]; read {
		return property, property != nil
	}
	var irProperty *ir.Property
	if model, ok := w.Stands(id).(*ir.Model); ok {
		if decl, held := w.names.Member(model.ID, wire); held {
			irProperty = decl.Property
		}
	}
	w.taken[key] = irProperty
	return irProperty, irProperty != nil
}

// Walk returns the property each token names, from the model root through the
// type of each member it reaches, and false where a step reaches no model, or
// a model declaring no member of that name.
func (w *CompiledDocumentView) Walk(root ir.TypeID, tokens []string) ([]ir.PropertyID, bool) {
	segments := make([]ir.PropertyID, 0, len(tokens))
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

// Members returns the members of the model id stands for (Stands), in layout
// order (ir.FlattenedProperties), and none for any other type. It flattens
// each model once, however many aliases name it, for a step that reads every
// member; one asking for a name reads it with Member, which flattens nothing.
func (w *CompiledDocumentView) Members(id ir.TypeID) []*ir.Property {
	model, ok := w.Stands(id).(*ir.Model)
	if !ok {
		return nil
	}
	if properties, read := w.members[model.ID]; read {
		return properties
	}
	var irProperties []*ir.Property
	if flat := w.flatten(w.Types, model.ID); len(flat.Properties) > 0 {
		irProperties = make([]*ir.Property, len(flat.Properties))
		for i := range flat.Properties {
			irProperties[i] = flat.Properties[i].Property
		}
	}
	w.members[model.ID] = irProperties
	return irProperties
}

// Params returns operation's parameters by where its HTTP bindings place them,
// indexed at the first ask and kept.
func (w *CompiledDocumentView) Params(operation *ir.Operation) ParamIndex {
	ix, indexed := w.params[operation.ID]
	if !indexed {
		ix = indexParams(operation)
		w.params[operation.ID] = ix
	}
	return ix
}

// Header returns the headers among headers that declare name, its case aside,
// as HTTP compares header names. headers are those of the response at names:
// the first read of a response indexes them and a later read takes that index,
// so reads of one key pass the same headers.
func (w *CompiledDocumentView) Header(at ResponseKey, headers []ir.Header, name string) []ir.PropertyID {
	byName, indexed := w.headers[at]
	if !indexed {
		byName = make(map[string][]ir.PropertyID, len(headers))
		for i := range headers {
			property := &headers[i].Property
			key := strings.ToLower(property.EffectiveWireName())
			byName[key] = append(byName[key], property.ID)
		}
		w.headers[at] = byName
	}
	return byName[strings.ToLower(name)]
}
