package compile

import (
	"context"
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// Types owns the type registry and the source-coordinate to ir.TypeID map that
// together keep invariant 3 true: one node per source coordinate, addressed by a
// stable synthetic ID.
//
// The zero value is not usable; call NewTypes. A Types is the mutable state of a
// single compile and is not safe for concurrent use — compilers are pure and
// reentrant because each call builds its own, not because this is synchronized.
type Types struct {
	reg       ir.TypeRegistry
	byPointer map[string]ir.TypeID
	// refused holds why each rejected entry was rejected, and reported how many
	// of them Finish has reported.
	refused  []string
	reported int
	// spaces records how each namespace is addressed — true for minted, false
	// for source-addressed — so claimSpace can catch one namespace used both
	// ways whichever way round it happens.
	spaces map[Space]bool
	// byID is byPointer read backwards, so a derivation that maps two distinct
	// coordinates onto one ID is caught rather than silently overwriting.
	byID map[ir.TypeID]string
	// promised holds every coordinate Promise queued, and promises those not
	// yet taken; fulfilled records that Finish has built them. See Promise.
	promised  map[string]bool
	promises  map[string]func() []ir.Diagnostic
	fulfilled bool
	// pending holds the settlements Defer queued for Finish, and settled
	// records that Finish has taken them. See Settlement.
	pending []Settlement
	settled bool
}

// refuse records why an entry was rejected. The registry declines to hold it
// rather than returning an error, because the caller is mid-walk with no useful
// recovery — but declining silently would make this type the one place able to
// manufacture the malformed states irverify and pass.Validate exist to report.
// Finish reports them.
func (t *Types) refuse(format string, args ...any) {
	t.refused = append(t.refused, fmt.Sprintf(format, args...))
}

// RegistryRefused is the code of the error Finish reports for an entry the
// registry refused. It names a compiler bug no source can produce, so it
// locates nothing in a source.
const RegistryRefused = "compile/registry-refused"

// Finish ends a compile's use of t, after its last lowering. It builds each
// promised node no lowering built (Promise), takes every deferred Settlement,
// then hands over the Document's registry and the diagnostics to report: what
// building and settling reported, then a RegistryRefused error for each entry
// t refused. The refusals ride in the list a compiler reports anyway, so it
// cannot take the registry and lose them.
//
// Each refusal is reported once, and one made after Finish only by a later
// call. A cancelled ctx stops it between steps, and leaves a registry the
// caller must not assemble.
func (t *Types) Finish(ctx context.Context) (ir.TypeRegistry, []ir.Diagnostic) {
	diags := t.fulfil(ctx)
	diags = append(diags, t.settle(ctx)...)
	for _, why := range t.refused[t.reported:] {
		diags = append(diags, ir.NewDiagnostic(ir.SeverityError, RegistryRefused,
			"internal: the type registry refused an entry: "+why, ir.Provenance{Source: ir.NoSource}))
	}
	t.reported = len(t.refused)
	return t.reg, diags
}

// NewTypes returns an empty registry.
func NewTypes() *Types {
	return &Types{
		reg:       ir.TypeRegistry{},
		byPointer: make(map[string]ir.TypeID),
		spaces:    make(map[Space]bool),
		byID:      make(map[ir.TypeID]string),
		promised:  make(map[string]bool),
		promises:  make(map[string]func() []ir.Diagnostic),
	}
}

// claimID records which coordinate owns id, and refuses a second coordinate
// claiming the same one.
//
// This is the other half of invariant 3: claimSpace catches a minted node
// landing where a source coordinate can reach it, and this catches a derivation
// that collapses two distinct coordinates onto one ID, such as a broken pointer
// escape or a dropped path segment.
//
// Nothing downstream sees it. Intern keys by coordinate, so the second node
// silently overwrites the first, and the survivor is well-formed and passes
// every structural check. Only the count of interned types shows it.
func (t *Types) claimID(id ir.TypeID, pointer string) {
	owner, claimed := t.byID[id]
	if claimed && owner != pointer {
		t.refuse("id %q is derived from both %q and %q; one derivation collapses two coordinates",
			id, owner, pointer)
		return
	}
	t.byID[id] = pointer
}

// claimSpace records whether id's namespace holds minted or source-addressed
// nodes, and refuses the second one to arrive when they disagree.
//
// This is invariant 3's corollary made mechanical: a node a lowering mints must
// occupy a namespace no source coordinate can produce. Sharing one makes the
// two race for a single ID, won by whichever declaration lowered first; the
// document is well-formed either way, so no check of one document sees it.
//
// It checks the namespace rather than the ID because a minted ID that does not
// collide today collides once the source names one more position.
func (t *Types) claimSpace(id ir.TypeID, minted bool) {
	space := spaceOf(id)
	if space == "" {
		return // no space segment to share; the ID grammar is checked elsewhere
	}
	if was, seen := t.spaces[space]; seen && was != minted {
		t.refuse("namespace %q holds both minted and source-addressed nodes (at id=%q); "+
			"a minted node needs a namespace of its own", space, id)
		return
	}
	t.spaces[space] = minted
}

// Intern returns the ID for pointer, calling build on first visit only.
//
// The ID is recorded before build runs, which is what terminates recursive and
// diamond schemas: a self-reference reached while building hits the map and
// returns the ID rather than re-entering build. Until build returns the node is
// Building, and NodeAt has none to give.
//
// A second call for the same pointer returns the first ID and does not rebuild,
// so a caller must not rely on build running — it is the interning table, not a
// constructor.
func (t *Types) Intern(pointer string, id ir.TypeID, build func() ir.TypeDef) ir.TypeID {
	if existing, ok := t.byPointer[pointer]; ok {
		return existing
	}
	if pointer == "" || id == "" || build == nil {
		t.refuse("intern rejected: pointer=%q id=%q build==nil=%v", pointer, id, build == nil)
		return id
	}

	t.claimSpace(id, false)
	t.claimID(id, pointer)
	t.byPointer[pointer] = id
	td := build() // may recurse; a self-reference hits byPointer above
	if ir.IsNilTypeDef(td) {
		// Leaving the coordinate mapped would be the one state NodeAt's contract
		// rules out: a pointer that resolves to an ID holding no node.
		delete(t.byPointer, pointer)
		delete(t.byID, id)
		t.refuse("intern rejected: build returned a nil type definition for id=%q at %q", id, pointer)
		return id
	}
	t.reg[id] = td
	return id
}

// Register records td under id without associating it with any source
// coordinate.
//
// It exists for nodes a lowering mints rather than finds, such as a composed
// union variant: its coordinate already denotes the branch schema it was built
// from, so interning it there would race for one pointer. Such nodes take a
// synthetic ID and no coordinate entry.
//
// Prefer Intern for a node that has a source coordinate: it enforces one node
// per coordinate and terminates recursion. Register overwrites a colliding ID,
// since a synthetic collision is a minting bug, not a revisit. claimSpace
// checks the namespace.
func (t *Types) Register(id ir.TypeID, td ir.TypeDef) {
	if id == "" {
		t.refuse("register rejected: empty type id")
		return
	}
	if ir.IsNilTypeDef(td) {
		t.refuse("register rejected: nil type definition for id=%q", id)
		return
	}
	// A namespace refusal does not withhold the node: the diagnostic names the
	// cause, and dropping the node would add dangling references on top of it —
	// a second symptom of the same bug, further from its source.
	t.claimSpace(id, true)
	t.reg[id] = td
}

// Lookup returns the ID interned at pointer, if any.
func (t *Types) Lookup(pointer string) (ir.TypeID, bool) {
	id, ok := t.byPointer[pointer]
	return id, ok
}

// NodeAt returns the node interned at pointer, and false when there is none:
// nothing interned the coordinate, or its node is still Building.
//
// It exists so "resolve the coordinate, then fetch the node" is one step with
// one answer. While its build runs a coordinate resolves, which is what stops
// recursion, but holds no node, so NodeAt reports false there rather than hand
// back a nil node to write to (GitHub #749).
func (t *Types) NodeAt(pointer string) (ir.TypeDef, bool) {
	id, ok := t.byPointer[pointer]
	if !ok {
		return nil, false
	}
	td, built := t.reg[id]
	return td, built
}

// Node returns the type definition registered under id, if any.
func (t *Types) Node(id ir.TypeID) (ir.TypeDef, bool) {
	td, ok := t.reg[id]
	return td, ok
}

// Building reports whether Intern is still building the node id names: the ID
// is claimed, so a revisit of its coordinate gets it back, but build has not
// returned it a node.
//
// A walk reaches such a node again only from below the frame building it, and
// that frame finishes the node once its build returns. A reader that finds no
// node asks this to tell the two misses apart: one is that frame's work, the
// other a node that will never exist.
func (t *Types) Building(id ir.TypeID) bool {
	if _, claimed := t.byID[id]; !claimed {
		return false
	}
	_, built := t.reg[id]
	return !built
}

// PrimRef interns the primitive of kind k on first use and returns a reference
// to it. Primitives are leaves reached by kind rather than by position, so they
// never enter the pointer-keyed table.
//
// Its Provenance.Source is ir.NoSource: one primitive is shared by every
// position of its kind in every source, so no single file declared it (GitHub
// #528). irverify does not yet hold other producers to this (GitHub #590).
//
// It claims neither ID nor space, since both claims are about a coordinate
// owning an ID. Another node landing in the prim space is caught, for every
// producer, by irverify's ir/prim-space-reserved.
func (t *Types) PrimRef(k ir.PrimKind) ir.TypeRef {
	id := ir.PrimTypeID(k)
	if _, ok := t.reg[id]; !ok {
		t.reg[id] = &ir.Primitive{
			ID:         id,
			Provenance: ir.Provenance{Source: ir.NoSource},
			Prim:       k,
		}
	}
	return ir.TypeRef{Target: id}
}

// PrimID interns the primitive of kind k and returns its ID.
func (t *Types) PrimID(k ir.PrimKind) ir.TypeID { return t.PrimRef(k).Target }

// Len reports how many types are interned.
func (t *Types) Len() int { return len(t.reg) }

// String renders the registry size, for use in error and debug output.
func (t *Types) String() string {
	return fmt.Sprintf("compile.Types{types: %d, coordinates: %d}", len(t.reg), len(t.byPointer))
}
