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
	// an $id names, which it is read from; Hop.Detail is the resource's URI.
	NoResourceTarget
	// NotSchema is a node no schema reference may name: an object of the
	// grammar, or a value that is neither a mapping nor a boolean.
	NotSchema
	// NotObject is a node a reference object's $ref names that is no object of
	// the kind Hop.Detail names. The parser resolves those, not the binder, so
	// only load words a failure by it, as a schema's NotSchema is worded.
	NotObject
	// NoAnchor is a plain-name fragment no schema of the resource it names
	// declares as its $anchor or $dynamicAnchor.
	NoAnchor
	// DuplicateAnchor is a plain-name fragment two or more schemas of the
	// resource it names declare, so it names none.
	DuplicateAnchor
	// AnchorUnread is a plain-name fragment naming a resource of another
	// document, whose identifiers are not read (GitHub #1049).
	AnchorUnread
	// DuplicateID is a URI two or more schemas declare as their $id, so it
	// names none; Hop.Detail is the URI.
	DuplicateID
	// BadPointer is a fragment that is no JSON pointer and no $anchor name: an
	// invalid escape, bytes that are not UTF-8, or a second '#', which RFC
	// 3986 admits in no fragment.
	BadPointer
	// NoDefinition is a "#/$defs/..." pointer the $defs rule finds nothing
	// for.
	NoDefinition
	// OwnDefinition is a "#/$defs/..." pointer the $defs rule finds nothing
	// for, which the schema writing it defines: its own definitions answer
	// only where an $id makes it a resource of its own, whose root the pointer
	// is then read from (resources.holding), as OAS 3.1 reads one with none
	// against the document.
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
	// BesideRef is a pointer through the fields beside a path item's $ref;
	// Hop.Use and Hop.Key name them.
	BesideRef
	// BesideDefs is a "#/$defs/..." pointer written in the fields beside a path
	// item's $ref, as any definition it can name is; Hop.Use and Hop.Key name
	// them.
	BesideDefs
	// Unpassable is a pointer through a reference object, in whichever
	// document, that resolves to nothing the compiler reads.
	Unpassable
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
	// Ref is the $ref as written.
	Ref    string
	Status Status
	Reason Reason
	// Detail is a read error, for Unreadable, a parse error, for Unparsable,
	// and the URI named, for NoResourceTarget and DuplicateID.
	Detail string
	// Use and Key are the path item $ref and the key written beside it, for
	// BesideRef.
	Use jsontext.Pointer
	Key string
	// Target is the position the hop lands on, and Role what it reads there.
	Target Position
	Role   Role
	// Named reports whether the $ref names the source, and Spelled is then the
	// pointer it spells from the source's root: where the lowering places what
	// it names, though a reference object it passes may carry Target into
	// another document.
	Named   bool
	Spelled jsontext.Pointer
}

// Chain is where a reference's chain of $refs ends: following each hop until a
// position holds no $ref.
type Chain struct {
	Status Status
	// End is the position the chain ends on, when Resolved; Hops counts the
	// $refs read to reach it.
	End  Position
	Hops int
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
// reported: the least of them, in whichever document.
type Loop struct {
	Members []Position
	Site    Position
}

// Landing is a position some hop reads as a schema though the parser's model
// holds none there, which a schema has to be built for: raw YAML in the
// source, or a node of another document, whose Name it is in. Site is the
// least position whose chain reaches it, a $ref's or a mapping value's, in
// whichever document; Reached is false when none does, as where the budget
// cut the reading.
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
	if h.Status == OverBudget {
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
		return "no schema of the resource it names declares that name as its $anchor or $dynamicAnchor"
	case DuplicateAnchor:
		return "two or more schemas of the resource it names declare that name as their $anchor or " +
			"$dynamicAnchor, so it names none of them"
	case AnchorUnread:
		return "it names a plain name in another document, whose identifiers are not read (GitHub #1049)"
	case DuplicateID:
		return "two or more schemas declare " + h.Detail + " as their $id, so it names none of them"
	case BadPointer:
		return "its fragment is no JSON pointer"
	case NoDefinition:
		return "no definition answers it: neither the document's root nor a schema of the model above it " +
			"holds one by that name"
	case OwnDefinition:
		return "the schema writing it defines that name, but a schema's own definitions answer a " +
			"\"#/$defs/...\" pointer only where an $id makes it a resource of its own, and neither the " +
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
	case BesideRef:
		return "it reaches " + string(h.Spelled) + "; " + BesideReason(h.Use, h.Key)
	case BesideDefs:
		return "it is written in the fields beside a path item $ref, as is any definition it can name; " +
			BesideReason(h.Use, h.Key)
	case Unpassable:
		return "its pointer passes through a reference that resolves to nothing the compiler reads"
	case MergeBound:
		return "it reads a key that may lie past a `<<` merge chain deeper than the " +
			strconv.Itoa(nodeview.MergeDepthLimit) + " levels the compiler expands"
	default:
		return "it does not resolve"
	}
}

// BesideReason says why a reference reaching the fields beside the path item
// $ref at use, under key, is left unresolved, in one phrasing for every
// report.
func BesideReason(use jsontext.Pointer, key string) string {
	return "the path item $ref at " + string(use) + " has " + strconv.Quote(key) + " written beside it, and " +
		"the resolver reads a pointer through that $ref into the item it names, not into the field written " +
		"there, so the reference is left unresolved (GitHub #824)"
}
