// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// principalKept is what the readers of an inline principal and the position
// its node stands at tell each other while that node is finished, since two
// schemas keep keywords in its one Unmodeled: the readers say what they will
// keep, the position writes its own and answers, and the readers then keep
// against what it wrote (keepAround,
// docs/compilers/openapi.md#allof-inline-residue).
type principalKept struct {
	// shape is how the reader that built the node names it in a report
	// (positionShape), which the position's reports say too.
	shape string
	// keys are the keywords the readers keep verbatim once the position has
	// written, in the order said, and reason the one they share (sharedOf).
	keys   []string
	reason ir.UnmodeledReason
	// whole is the position's answer: it keeps the principal whole, under its
	// entry's key, which holds keys.
	whole bool
	// apart are the keywords kept under the principal's entry's key so far,
	// written otherwise beside the allOf, and apartReason the one they share.
	apart       []string
	apartReason ir.UnmodeledReason
}

// keeping is what one reader keeps verbatim of a schema on the node it lowered
// to: keys, for reason, each under its own key by keep, which reports them as
// the same keywords written flat are reported, naming the node shape where the
// reader names one.
type keeping struct {
	keys   []string
	reason ir.UnmodeledReason
	shape  string
	keep   func(p *ir.Unmodeled, keys []string) []ir.Diagnostic
}

// saying returns h, allocated where nil, having heard that a reader will keep
// k's keys.
func (h *principalKept) saying(k keeping) *principalKept {
	if h == nil {
		h = &principalKept{}
	}
	h.shape = cmp.Or(k.shape, h.shape)
	if len(k.keys) == 0 {
		return h
	}
	h.reason = k.reason
	if len(h.keys) > 0 {
		h.reason = sharedOf(h.reason, k.reason)
	}
	h.keys = append(h.keys, k.keys...)
	return h
}

// named returns how the principal's readers name the node standing for their
// position, "" where none said.
func (h *principalKept) named() string {
	if h == nil {
		return ""
	}
	return h.shape
}

// sharedOf is the reason an entry holding what a and b are kept for is kept
// under: the one they share, else degraded_lowering, as sharedReason reads it.
func sharedOf(a, b ir.UnmodeledReason) ir.UnmodeledReason {
	if a == b {
		return a
	}
	return ir.ReasonDegradedLowering
}

// keepAround finishes td for one reader of s, written at pointer, keeping k
// on it. On the schema's own node it keeps, then runs fin, the frames outside.
// On the node standing for st's position it says what it will keep, lets fin,
// the position's finisher among it, write first, and keeps after
// (keepStanding): neither schema can know what the other keeps before it has.
func keepAround(c lowering.Ctx, ts *compile.Types, st stand, td ir.TypeDef, read keySet, held *principalKept,
	s *statement.Schema, pointer jsontext.Pointer, k keeping, fin finisher,
) []ir.Diagnostic {
	p := &td.Common().Unmodeled
	if st.of == nil {
		diags := k.keep(p, k.keys)
		fin.run(td, read, held)
		return diags
	}
	held = held.saying(k)
	fin.run(td, read, held)
	return keepStanding(c, ts, allOfBranches(c, st.of, st.at), held, p, s, pointer, k)
}

// keepStanding keeps k, what one reader keeps of s, the inline principal of
// comp written at pointer, on p, the Unmodeled of the node standing for comp's
// position, which that position has written: nothing where it keeps s whole,
// the entry holding them, else each keyword as heldAgainst places it, one
// whose own key is free as k keeps it, and one that key holds otherwise under
// s's entry's key (keepApart). This is the one way a keyword of s reaches the
// node once the position has written. A discriminator resolves its routes
// wherever it is kept (routesKept).
func keepStanding(c lowering.Ctx, ts *compile.Types, comp allOfComposition, held *principalKept, p *ir.Unmodeled,
	s *statement.Schema, pointer jsontext.Pointer, k keeping,
) []ir.Diagnostic {
	if held.whole {
		return routesKept(c, ts, s, k.keys, pointer)
	}
	plain, apart := heldAgainst(*p, s, k.keys)
	diags := k.keep(p, plain)
	if slices.Contains(plain, "discriminator") {
		// The discriminator kept whole holds what the position read out of it.
		promotion.DropContained(c.Promoter(), p, annotation.EntryKey("", "discriminator"))
	}
	return append(diags, keepApart(c, ts, comp, held, p, s, pointer, apart, k.reason)...)
}

// heldAgainst splits keys, keywords the inline principal s keeps, by what p,
// the Unmodeled its position has written, holds under each one's own key:
// plain where that key is free, apart where it holds another value than s
// writes, and neither where it holds the same, kept once. Two values are the
// same where they render to the same JSON, a number's spelling and an array's
// order with them, as two entries' are (writtenKeys.note); one that cannot
// render is plain, its keeper's to refuse as it refuses one written flat.
func heldAgainst(p ir.Unmodeled, s *statement.Schema, keys []string) (plain, apart []string) {
	for _, key := range keys {
		entry, taken := p[annotation.EntryKey("", key)]
		if !taken {
			// The path nearly every keyword takes: nothing to render.
			plain = append(plain, key)
			continue
		}
		raw, err := annotation.RawFromNode(keyNode(s, key))
		switch {
		case err != nil:
			plain = append(plain, key)
		case !bytes.Equal(raw, entry.Value):
			apart = append(apart, key)
		}
	}
	return plain, apart
}

// keepApart keeps on p, the Unmodeled of the node standing for the position
// comp is written at, the keywords apart that s, comp's inline principal
// written at pointer, writes otherwise than the position keeps them: under its
// entry's key, openapi:allOf/<n>, one object holding those and what held says
// readers before kept there, the rest of s lowered, for the reason they share,
// at the entry, with one report naming apart. A discriminator among them
// resolves its routes, as one kept anywhere does (resolveDiscriminatorRoutes).
func keepApart(c lowering.Ctx, ts *compile.Types, comp allOfComposition, held *principalKept, p *ir.Unmodeled,
	s *statement.Schema, pointer jsontext.Pointer, apart []string, reason ir.UnmodeledReason,
) []ir.Diagnostic {
	if len(apart) == 0 {
		return nil
	}
	kept, diags := holdApart(c, comp, held, p, s, pointer, apart, reason)
	if !kept {
		return diags
	}
	// A carrier keeps the key beneath its own scope, so the report names the
	// entry's path, which every such key ends in.
	diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"the keywords beside %s write %s otherwise than this inline branch, and one entry under a keyword holds "+
			"one value; the branch's are kept verbatim under Unmodeled under the key naming its entry, %s, the rest of it lowered",
		comp.subject(), strings.Join(apart, ", "), comp.branchPath(pointer)))
	return append(diags, routesKept(c, ts, s, apart, pointer)...)
}

// holdApart writes on p the object the entry's key of s, comp's inline
// principal written at pointer, holds: the keywords apart and those held says
// were kept there before, as s writes them, for the reason they share. It
// reports whether it wrote, and held then counts apart among them.
func holdApart(c lowering.Ctx, comp allOfComposition, held *principalKept, p *ir.Unmodeled, s *statement.Schema,
	pointer jsontext.Pointer, apart []string, reason ir.UnmodeledReason,
) (bool, []ir.Diagnostic) {
	all, shared := slices.Concat(held.apart, apart), reason
	if len(held.apart) > 0 {
		shared = sharedOf(held.apartReason, reason)
	}
	members := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range s.Keys {
		if slices.Contains(all, k.Name) {
			members.Content = append(members.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k.Name}, k.Node)
		}
	}
	kept, diags := annotation.PreserveNodeInto(p, comp.branchKey(pointer), members, shared, c.Place().At(pointer))
	if kept {
		held.apart, held.apartReason = all, shared
	}
	return kept, diags
}

// keepBuiltAround finishes td by fin, the frames outside every reader of the
// inline principal s written at pointer, the position's among them, holding
// back what the principal's build kept on td itself: a keyword kept beside the
// field it could not fill, which no reader keeps after the position. Those
// entries leave the node while the position writes, and are kept after it
// against what it wrote (keepBuilt), so whatever a build keeps reaches the
// node through the one writer too.
func keepBuiltAround(c lowering.Ctx, st stand, td ir.TypeDef, read keySet, held *principalKept, s *statement.Schema,
	pointer jsontext.Pointer, fin finisher,
) []ir.Diagnostic {
	p := &td.Common().Unmodeled
	built := takeBuilt(p, pointer)
	held = held.saying(keeping{})
	fin.run(td, read, held)
	return keepBuilt(c, allOfBranches(c, st.of, st.at), held, p, s, pointer, built)
}

// takeBuilt removes from p, and returns, the entries written at pointer or
// beneath it: what the schema written there kept on the node it built.
func takeBuilt(p *ir.Unmodeled, pointer jsontext.Pointer) ir.Unmodeled {
	var built ir.Unmodeled
	for key, entry := range *p {
		at := entry.Provenance.Pointer
		if at != pointer && !strings.HasPrefix(string(at), string(pointer)+"/") {
			continue
		}
		if built == nil {
			built = ir.Unmodeled{}
		}
		built[key] = entry
		delete(*p, key)
	}
	return built
}

// keepBuilt keeps on p, which the position has written, the entries built,
// what the build of s, comp's inline principal written at pointer, kept: each
// under its own key where that is free; once where the key holds the same
// value, or the position keeps s whole, the entry holding it; and under s's
// entry's key where the position's keyword holds another (holdApart). The
// build reported each where it kept it, as written flat, which stays true, so
// nothing more is said. A key no keyword of s names cannot be held otherwise.
func keepBuilt(c lowering.Ctx, comp allOfComposition, held *principalKept, p *ir.Unmodeled, s *statement.Schema,
	pointer jsontext.Pointer, built ir.Unmodeled,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, key := range slices.Sorted(maps.Keys(built)) {
		entry := built[key]
		cur, taken := (*p)[key]
		keyword := strings.TrimPrefix(key, annotation.EntryKey("", ""))
		switch {
		case !taken:
			annotation.PreserveInto(p, key, entry.Value, entry.Reason, entry.Provenance)
		case held.whole || bytes.Equal(cur.Value, entry.Value):
		case !hasKey(s, keyword):
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
				"internal: %s, kept by the build of this allOf branch, is held with another value", key))
		default:
			_, heldDiags := holdApart(c, comp, held, p, s, pointer, []string{keyword}, entry.Reason)
			diags = append(diags, heldDiags...)
		}
	}
	if _, kept := built[annotation.EntryKey("", "discriminator")]; kept || held.whole && len(built) > 0 {
		// What the position read out of a discriminator kept whole, and what a
		// whole entry holds, is kept once (dropFoldedIntoWhole).
		promotion.DropContained(c.Promoter(), p, annotation.EntryKey("", "discriminator"))
		dropFoldedIntoWhole(c, p)
	}
	return diags
}

// keyWrittenAlike reports whether a and b write key with one value, as heldAgainst
// compares two; one that cannot render is alike to nothing.
func keyWrittenAlike(a, b *statement.Schema, key string) bool {
	if !hasKey(a, key) || !hasKey(b, key) {
		return false
	}
	x, errA := annotation.RawFromNode(keyNode(a, key))
	y, errB := annotation.RawFromNode(keyNode(b, key))
	return errA == nil && errB == nil && bytes.Equal(x, y)
}

// routesKept resolves the mapping of the discriminator s, written at pointer,
// writes where kept, keywords kept another way than keepUnhomed keeps them,
// holds it: a discriminator kept verbatim resolves its routes wherever it is
// kept, as keepUnhomed resolves the one it keeps (resolveDiscriminatorRoutes).
func routesKept(c lowering.Ctx, ts *compile.Types, s *statement.Schema, kept []string, pointer jsontext.Pointer) []ir.Diagnostic {
	if !slices.Contains(kept, "discriminator") {
		return nil
	}
	return resolveDiscriminatorRoutes(c, ts, s, pointer)
}

// outcome returns what st's position keeps, having lowered to the node it owns.
func (st stand) outcome(c lowering.Ctx) outcomeKeeps {
	return keepsAtOutcome(c, st.of, ir.TypeRef{Target: c.IDs().ForPointer(st.at)}, st.at)
}

// heldWhole reports whether st's position keeps the inline principal written at
// pointer whole, under its entry's key. A type list asks before it builds its
// variants, which no finisher reaches; every other reader of the principal is
// told (principalKept.whole).
func (st stand) heldWhole(c lowering.Ctx, pointer jsontext.Pointer) bool {
	return st.of != nil && st.outcome(c).holdsWhole(pointer)
}

// holdsWhole reports whether k keeps the entry written at pointer whole.
func (k outcomeKeeps) holdsWhole(pointer jsontext.Pointer) bool {
	return slices.ContainsFunc(k.entries, func(e keptEntry) bool { return e.entry.pointer == pointer && e.keptWhole() })
}
