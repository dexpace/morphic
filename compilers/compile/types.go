package compile

import (
	"fmt"
	"reflect"

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
}

// isNilTypeDef reports whether td is a nil TypeDef — an untyped nil interface or
// a typed nil pointer. A typed nil satisfies a type switch case, so a caller
// cannot screen one by kind.
func isNilTypeDef(td ir.TypeDef) bool {
	if td == nil {
		return true
	}
	rv := reflect.ValueOf(td)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
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
	}
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
	t.byPointer[pointer] = id
	td := build() // may recurse; a self-reference hits byPointer above
	if isNilTypeDef(td) {
		// Leaving the coordinate mapped would be the one state NodeAt's contract
		// rules out: a pointer that resolves to an ID holding no node.
		delete(t.byPointer, pointer)
		t.refuse("intern rejected: build returned a nil type definition for id=%q at %q", id, pointer)
		return id
	}
	t.reg[id] = td
	return id
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
	if isNilTypeDef(td) {
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
func (t *Types) PrimRef(k ir.PrimKind) ir.TypeRef {
	id := PrimTypeID(k)
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
