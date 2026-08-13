package compile

import (
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
	src       int
	refused   []string
	// spaces records how each namespace is addressed — true for minted, false
	// for source-addressed — so claimSpace can catch one namespace used both
	// ways whichever way round it happens.
	spaces map[Space]bool
	// byID is byPointer read backwards, so a derivation that maps two distinct
	// coordinates onto one ID is caught rather than silently overwriting.
	byID map[ir.TypeID]string
	// provisional holds the coordinates whose node was named by a lowering that
	// reached them through a reference rather than through the declaration that
	// owns them. See InternProvisional.
	provisional map[string]bool
}

// refuse records why an entry was rejected. The registry declines to hold it
// rather than returning an error, because the caller is mid-walk with no useful
// recovery — but declining silently would make this type the one place able to
// manufacture the malformed states irverify and pass.Validate exist to report.
// Violations surfaces them.
func (t *Types) refuse(format string, args ...any) {
	t.refused = append(t.refused, fmt.Sprintf(format, args...))
}

// Violations returns the invariant breaches the registry refused to record, in
// the order they were attempted.
//
// A non-empty result is a compiler bug, not a spec problem: every entry names an
// empty ID, an empty coordinate, or a nil type definition, none of which any
// source can produce. The owning compiler surfaces them as internal-invariant
// diagnostics rather than dropping them.
func (t *Types) Violations() []string { return t.refused }

// NewTypes returns an empty registry whose interned primitives are stamped with
// source index src.
func NewTypes(src int) *Types {
	return &Types{
		reg:       ir.TypeRegistry{},
		byPointer: make(map[string]ir.TypeID),
		src:       src,
		spaces:    make(map[Space]bool),
		byID:      make(map[ir.TypeID]string),

		provisional: make(map[string]bool),
	}
}

// claimID records which coordinate owns id, and refuses a second coordinate
// claiming the same one.
//
// This is the other half of invariant 3, and neither half implies the other.
// claimSpace catches a minted node landing where a source coordinate can reach
// it; this catches a derivation that *collapses* two distinct coordinates onto
// one ID — a broken pointer escape, a path segment dropped, a space that stopped
// distinguishing what it was meant to.
//
// Nothing downstream can see it happen. Intern keys by coordinate, so both
// pointers map cleanly and the second node simply overwrites the first in the
// registry; the survivor is well-formed, carries its own provenance, and passes
// every structural check — including the ID-to-provenance agreement, which the
// loser is no longer present to fail. The count of interned types is the only
// trace, and nothing knows what it should have been.
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
// occupy a namespace no source coordinate can produce. Sharing one leaves the two
// racing for a single ID, and the winner is whichever declaration lowered first —
// silently, because the document is well-formed either way. Reversing the branch
// order of a colliding spec produces no diagnostic, irverify is clean, and
// pass.Validate passes; only a golden diff shows it, and regenerating the golden
// makes it green again.
//
// The check is at the namespace rather than the ID because a minted ID that
// happens not to collide today collides as soon as the source names one more
// position — safety that rests on which paths a format's pointers cannot spell is
// the reasoning the corollary exists to replace.
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
// returns the ID rather than re-entering build.
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

// InternProvisional is Intern for a lowering that reached pointer through a
// reference naming it rather than through the declaration that owns it, and
// records that the name the node is being given is a placeholder.
//
// A reference can name a coordinate inside another declaration's body, and both
// lowerings reach it: the declaration through its own structure, the reference
// through the pointer it spells. Intern calls build for whichever arrives first,
// so the node's name used to be decided by declaration order — silently, since
// either spelling is a valid name and nothing compared them.
//
// Only the *name* is a question the declaration answers better; the node itself
// is the same one either way. So the reference still builds it, and
// NameFromDeclaration replaces the name when the declaration arrives — in
// whichever order the two happen.
//
// A coordinate already interned is not marked: the declaration may have been
// there first, and a name it settled is not a placeholder.
func (t *Types) InternProvisional(pointer string, id ir.TypeID, build func() ir.TypeDef) ir.TypeID {
	_, before := t.byPointer[pointer]
	interned := t.Intern(pointer, id, build)
	if _, after := t.byPointer[pointer]; after && !before {
		t.provisional[pointer] = true
	}
	return interned
}

// NameFromDeclaration gives the node at pointer the hint its declaration
// derives, replacing a placeholder a reference left there first.
//
// It is a no-op for a coordinate that is not carrying a placeholder, which is
// every coordinate the declaration reached first — there the name is already the
// one this would write. That is also what makes a second declaration at one
// coordinate silent here rather than last-write-wins: two declarations claiming
// one coordinate is what claimID refuses, and re-reporting it as a naming
// problem would name the symptom instead of the cause.
func (t *Types) NameFromDeclaration(pointer, hint string) {
	if !t.provisional[pointer] {
		return
	}
	delete(t.provisional, pointer)
	// The coordinate resolves and its node is present: a coordinate is marked
	// provisional only once Intern has recorded both, and Intern is what removes
	// the pair again when a build yields nothing — so there is no state here in
	// which one exists without the other, and a branch for one would be
	// untestable (the reasoning NodeAt states).
	//
	// Neutralized on the way in, because this writes the field NamingHint would
	// have written and has to write it the same way. A raw hint here would leave
	// the node holding the caller's spelling — "A" where interning the same hint
	// gives "a" — so the name would depend on whether a reference got there
	// first, which is the dependence this whole path exists to remove.
	t.reg[t.byPointer[pointer]].Common().Name.Hint = neutralHint(hint)
}

// Register records td under id without associating it with any source
// coordinate.
//
// It exists for nodes a lowering mints rather than finds. A composed union
// variant is the case: the coordinate it would occupy already denotes the branch
// schema it was built from, so interning it there makes the two race for one
// pointer — whichever lowered first wins it, and the result depends on
// declaration order. Such nodes take a synthetic ID and no coordinate entry.
//
// Prefer Intern wherever the node does correspond to a source coordinate: that
// is the path enforcing one node per coordinate, and the one that terminates
// recursion. Register overwrites a colliding ID rather than deduplicating,
// because a synthetic ID that collides is a bug in the minting scheme, not a
// revisit.
//
// The namespace a minted node lands in is checked rather than assumed: see
// claimSpace.
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

// NodeAt returns the node interned at pointer.
//
// It exists so the two-step "resolve the coordinate, then fetch the node" cannot
// be written with a gap between the steps. Intern records a coordinate and its
// node together, so a coordinate that resolves always has a node — a caller
// doing Lookup then Node has to write a branch for a state this type does not
// produce, and an unreachable branch is worse than no branch: it cannot be
// tested, and it suggests the state is possible.
func (t *Types) NodeAt(pointer string) (ir.TypeDef, bool) {
	id, ok := t.byPointer[pointer]
	if !ok {
		return nil, false
	}
	return t.reg[id], true
}

// Node returns the type definition registered under id, if any.
func (t *Types) Node(id ir.TypeID) (ir.TypeDef, bool) {
	td, ok := t.reg[id]
	return td, ok
}

// PrimRef interns the primitive of kind k on first use and returns a reference
// to it. Primitives are leaves reached by kind rather than by position, so they
// never enter the pointer-keyed table.
//
// It writes the registry directly, claiming neither the ID nor the space: both
// claims are about a coordinate owning an ID, and a primitive has no coordinate.
// What that leaves unguarded here — another node landing in the prim space — is
// caught at the document boundary by irverify's ir/prim-space-reserved, which
// holds every producer rather than only a compile that went through this type.
func (t *Types) PrimRef(k ir.PrimKind) ir.TypeRef {
	id := ir.PrimTypeID(k)
	if _, ok := t.reg[id]; !ok {
		t.reg[id] = &ir.Primitive{
			TypeCommon: ir.TypeCommon{ID: id, Provenance: ir.Provenance{Source: t.src}},
			Prim:       k,
		}
	}
	return ir.TypeRef{Target: id}
}

// PrimID interns the primitive of kind k and returns its ID.
func (t *Types) PrimID(k ir.PrimKind) ir.TypeID { return t.PrimRef(k).Target }

// Registry returns the built registry for assembly into an ir.Document.
//
// It hands over the live map rather than a copy: the caller is the compiler that
// owns this Types, the value goes straight into a Document it is assembling, and
// copying a registry per compile to guard against a caller that has no reason to
// mutate it would cost the whole walk's allocation for nothing. Callers outside
// the owning compile must treat the result as read-only.
func (t *Types) Registry() ir.TypeRegistry { return t.reg }

// Len reports how many types are interned.
func (t *Types) Len() int { return len(t.reg) }

// String renders the registry size, for use in error and debug output.
func (t *Types) String() string {
	return fmt.Sprintf("compile.Types{types: %d, coordinates: %d}", len(t.reg), len(t.byPointer))
}
