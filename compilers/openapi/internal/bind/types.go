// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"cmp"
	"encoding/json/jsontext"
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// DocID names a document a table reads: Source, or one opened for a
// reference naming it, numbered in the order the table first needed it.
type DocID int

// Source is the document being compiled.
const Source DocID = 0

// Position is where a node is written: a document and the JSON pointer to the
// node from that document's root.
type Position struct {
	Doc     DocID
	Pointer jsontext.Pointer
}

// comparePositions orders positions by document, then pointer: the order a
// table reads them in where they share a document or one is the source's.
// Two other documents are ordered by name (docs.compare): a DocID numbers them
// in the order the table first needed them, which a declaration order changes.
func comparePositions(p, q Position) int {
	if c := cmp.Compare(p.Doc, q.Doc); c != 0 {
		return c
	}
	return cmp.Compare(p.Pointer, q.Pointer)
}

// PositionOrder returns the order t reads positions in: the source's first,
// another document's by its name, then by pointer, so an order by it depends
// on no order documents were read in.
func PositionOrder(t *Table) func(p, q Position) int {
	name := func(id DocID) string { return t.docs[id].Name }
	return func(p, q Position) int { return compareNamed(p, q, name) }
}

// compareNamed orders p and q as a table reads them, documents named by name
// (PositionOrder, docs.compare).
func compareNamed(p, q Position, name func(DocID) string) int {
	if c, ok := byName(p, q, name); ok {
		return c
	}
	return comparePositions(p, q)
}

// byName orders p and q, positions in two documents neither of which is the
// source, by the names name gives those documents.
func byName(p, q Position, name func(DocID) string) (int, bool) {
	if p.Doc == q.Doc || p.Doc == Source || q.Doc == Source {
		return 0, false
	}
	return cmp.Compare(name(p.Doc), name(q.Doc)), true
}

// Role is what a reference reads at the position it lands on.
type Role uint8

const (
	// NoRole is a position the document does not hold.
	NoRole Role = iota
	// Schema is a Schema Object the OpenAPI grammar places there, which the
	// parser's model holds.
	Schema
	// Raw is YAML the model keeps as written, or a node of another document: an
	// extension, a key no object defines, a literal such as an example. A
	// reference reads it as a schema.
	Raw
	// Object is any other object or value of the grammar, which no schema
	// reference may name.
	Object
)

// String spells r.
func (r Role) String() string {
	switch r {
	case Schema:
		return "schema"
	case Raw:
		return "raw"
	case Object:
		return "object"
	default:
		return "none"
	}
}

// Status is how a hop or a chain ended.
type Status uint8

const (
	// Resolved lands on a schema.
	Resolved Status = iota + 1
	// Unresolved lands on no schema; a chain is Unresolved when one of its hops
	// is.
	Unresolved
	// Cyclic is a chain that comes back to a position it already read.
	Cyclic
	// OverBudget is an answer the work budget ran out before.
	OverBudget
)

// statusNames spells each Status, in order.
var statusNames = []string{"resolved", "unresolved", "cyclic", "over-budget"}

// String spells s, or "invalid" for a value no constant names.
func (s Status) String() string {
	if s < Resolved || int(s) > len(statusNames) {
		return "invalid"
	}
	return statusNames[s-1]
}

// Reason is why a hop is Unresolved.
type Reason uint8

const (
	// NoReason is a hop that resolved.
	NoReason Reason = iota
	// NoTarget is a pointer that names no node of its document.
	NoTarget
	// NoResourceTarget is a pointer that names no node of the schema resource
	// an identifier names, which it is read from; Hop.Detail is the resource's
	// URI.
	NoResourceTarget
	// NotSchema is a node no schema reference may name: an object of the
	// grammar, or a value that is neither a mapping nor a boolean.
	NotSchema
	// NotObject is a node a reference object's $ref names that is no object of
	// the kind Hop.Detail names: an object the grammar reads as another kind,
	// or raw YAML that is no mapping.
	NotObject
	// NoAnchor is a plain-name fragment no schema of the resource it names
	// declares as its $anchor, its $dynamicAnchor or, in a draft before
	// 2019-09, an identifier naming that fragment alone (DialectRule.Fragments).
	NoAnchor
	// DuplicateAnchor is a plain-name fragment two or more schemas of the
	// resource it names declare, so it names none.
	DuplicateAnchor
	// DuplicateID is a URI two or more schemas declare as their identifier, a
	// $id or draft 4's id, so it names none; Hop.Detail is the URI.
	DuplicateID
	// BadPointer is a fragment that is no JSON pointer and no $anchor name: an
	// invalid escape, bytes that are not UTF-8, or a second '#', which RFC
	// 3986 admits in no fragment.
	BadPointer
	// NoDefinition is a "#/$defs/..." pointer the $defs rule finds nothing
	// for.
	NoDefinition
	// OwnDefinition is a "#/$defs/..." pointer the $defs rule finds nothing
	// for, which the schema writing it defines: its own definitions answer only
	// where an identifier makes it a resource of its own, whose root the
	// pointer is then read from (resources.holding), as OAS 3.1 reads one with
	// none against the document.
	OwnDefinition
	// RawDefinition is a "#/$defs/..." pointer the $defs rule finds nothing
	// for, which raw YAML above it defines: raw YAML holds no schema of the
	// model, so a schema under an extension answers none (GitHub #570).
	RawDefinition
	// ExternalOff is a reference to another document while none is read.
	ExternalOff
	// Unreadable is a reference to another document that could not be read;
	// Hop.Detail says why.
	Unreadable
	// Unparsable is a reference to another document that was read and does
	// not parse; Hop.Detail is the parser's account of where.
	Unparsable
	// Empty is a reference to another document that was read and holds
	// nothing (IsEmpty).
	Empty
	// Unpassable is a pointer through a reference object, in whichever
	// document, whose chain does not resolve, or whose resolution needs the
	// hop reading that pointer.
	Unpassable
	// Held is a reference to a document an absolute URI names, whose read waits
	// until the pass's walks are done. The pass then reads it and answers it,
	// unless the budget cuts it first.
	Held
	// MergeBound is a read that stopped at a mapping merging through `<<`
	// deeper than nodeview.MergeDepthLimit, so a key it needs may lie past the
	// bound: one the pointer names, an $id that may give the base it is read
	// against, an identifier it names, or a definition the $defs rule reads
	// before the one that would answer.
	MergeBound
)

// Hop is where the $ref written at a position lands: the next position of its
// chain, or why there is none.
type Hop struct {
	// Target is the position the hop lands on.
	Target Position
	// Ref is the $ref as written.
	Ref string
	// Detail is a read error, for Unreadable, a parse error, for Unparsable,
	// the URI named, for NoResourceTarget and DuplicateID, and the kind of
	// object asked for, for NotObject.
	Detail string
	// Status is how the hop ended, and Reason why, when it is Unresolved.
	Status Status
	Reason Reason
	// Role is what the hop reads at Target.
	Role Role
}

// Chain is where a reference's chain of $refs ends: following each hop until a
// position holds no $ref.
type Chain struct {
	// End is the position the chain ends on, when Resolved; Hops counts the
	// $refs read to reach it.
	End  Position
	Hops int
	// Status is how the chain ended.
	Status Status
	// Member reports, when Cyclic, whether the position itself lies on a loop
	// rather than leading into one.
	Member bool
}

// Entry is one position writing a schema $ref: its hop and its chain.
type Entry struct {
	Site  Position
	Hop   Hop
	Chain Chain
}

// Loop is a cycle of $refs: its members, ordered, and Site, where it is
// reported: the least of them, in whichever document. Kind is the kind of
// object a loop of reference objects stands for, and zero for one of schema
// $refs.
type Loop struct {
	Members []Position
	Site    Position
	Kind    RefKind
}

// RefKind is the kind of object a Reference Object stands for, which the
// position writing it reads and which its $ref must land on. The zero RefKind
// is none: a schema's $ref.
type RefKind uint8

// The kinds of object a Reference Object can stand for, in the order a
// position reading several is settled in.
const (
	RefPathItem RefKind = iota + 1
	RefParameter
	RefHeader
	RefRequestBody
	RefResponse
	RefCallback
	RefExample
	RefLink
	RefSecurityScheme
	RefMediaType
)

// refKindNames spells each RefKind, in order, as a message names one.
var refKindNames = []string{"path item", "parameter", "header", "request body", "response", "callback",
	"example", "link", "security scheme", "media type"}

// String spells k as a message names the kind of object, or "schema" for the
// zero RefKind and "object" for a value no constant names.
func (k RefKind) String() string {
	switch {
	case k == 0:
		return "schema"
	case int(k) > len(refKindNames):
		return "object"
	default:
		return refKindNames[k-1]
	}
}

// Reference is one position writing a Reference Object's $ref, read as Kind:
// its hop, which lands on an object of Kind or fails NotObject, and its chain.
type Reference struct {
	Site  Position
	Kind  RefKind
	Hop   Hop
	Chain Chain
}

// ObjectLanding is a position a Reference Object's chain lands on that the
// source's grammar reads as no object of Kind, which an object of Kind is
// built for: raw YAML of the source, such as an extension's value, or a node of
// another document, whose Name it is in. Site is the least position whose
// chain reaches it, in whichever document; Reached is false when none does, as
// where the budget cut the reading.
type ObjectLanding struct {
	At      Position
	Kind    RefKind
	Name    string
	Site    Position
	Reached bool
}

// Landing is a position the table reads as a schema though the parser's model
// holds none there, which a schema has to be built for: raw YAML in the
// source, or a node of another document, whose Name it is in, that some hop
// reads, or a tuple's entry or rest, which the model has no member for. Site
// is the least position whose chain reaches it, a $ref's, a mapping value's or
// an expanding $dynamicRef's, in whichever document, or the schema writing the
// tuple; Reached is false when none does, as where the budget cut the reading.
type Landing struct {
	At      Position
	Name    string
	Site    Position
	Reached bool
}

// Mapping is where a discriminator's mapping value lands, read as a $ref
// written at At, the discriminator's position.
type Mapping struct {
	At    Position
	Hop   Hop
	Chain Chain
}

// Why says why h, a hop that did not resolve, lands on no schema, in one
// phrasing for every report of it.
func (h Hop) Why() string {
	if h.Status == OverBudget || h.Reason == Held {
		return "it was not read within the budget"
	}
	switch h.Reason {
	case NoTarget:
		return "it names nothing in the document it points into"
	case NoResourceTarget:
		return "it names nothing in the schema resource " + h.Detail + ", whose root its pointer is read from"
	case NotSchema:
		return "it names a position that holds no schema"
	case NotObject:
		return "it names a position that holds no " + h.Detail
	case NoAnchor:
		return "no schema of the resource it names declares that name as its $anchor, its $dynamicAnchor " +
			"or, in a draft before 2019-09, an identifier naming that fragment alone"
	case DuplicateAnchor:
		return "two or more schemas of the resource it names declare that name as their $anchor, their " +
			"$dynamicAnchor or, in a draft before 2019-09, an identifier naming that fragment alone, so it " +
			"names none of them"
	case DuplicateID:
		return "two or more schemas declare " + h.Detail + " as their identifier, so it names none of them"
	case BadPointer:
		return "its fragment is no JSON pointer"
	case NoDefinition:
		return "no definition answers it: neither the document's root nor a schema of the model above it " +
			"holds one by that name"
	case OwnDefinition:
		return "the schema writing it defines that name, but a schema's own definitions answer a " +
			"\"#/$defs/...\" pointer only where an identifier makes it a resource of its own, and neither the " +
			"document's root nor a schema of the model above it holds one"
	case RawDefinition:
		return "raw YAML above it, such as an extension's value, defines that name, but raw YAML holds no " +
			"schema of the model whose definitions answer it, and neither the document's root nor a schema of " +
			"the model above it holds one"
	case ExternalOff:
		return "it names another document, and external references are not read"
	case Unreadable:
		return "the document it names could not be read: " + h.Detail
	case Unparsable:
		return "the document it names does not parse as YAML: " + h.Detail
	case Empty:
		return "the document it names is empty"
	case Unpassable:
		return "its pointer passes through a reference that does not resolve"
	case MergeBound:
		return "it reads a key that may lie past a `<<` merge chain deeper than the " +
			strconv.Itoa(nodeview.MergeDepthLimit) + " levels the compiler expands"
	default:
		return "it does not resolve"
	}
}
