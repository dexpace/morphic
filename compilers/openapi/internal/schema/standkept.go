// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
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
	// built is the reason what the principal's build kept on the node shares
	// (keepBuiltAround), "" for nothing: an entry kept whole holds that too.
	built ir.UnmodeledReason
}

// keeping is what one reader keeps verbatim of a schema on the node it lowered
// to: keys, for reason, each under its own key by keep, which reports them as
// the same keywords written flat are reported, naming the node shape where the
// reader names one.
type keeping struct {
	keys   []string
	reason ir.UnmodeledReason
	shape  string
	keep   func(unmodeled *ir.Unmodeled, keys []string) []ir.Diagnostic
}

// saying returns h, allocated where nil, having heard that a reader will keep
// keepingValue's keys.
func (h *principalKept) saying(keepingValue keeping) *principalKept {
	if h == nil {
		h = &principalKept{}
	}
	h.shape = cmp.Or(keepingValue.shape, h.shape)
	if len(keepingValue.keys) == 0 {
		return h
	}
	h.reason = keepingValue.reason
	if len(h.keys) > 0 {
		h.reason = sharedOf(h.reason, keepingValue.reason)
	}
	h.keys = append(h.keys, keepingValue.keys...)
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

// keepAround finishes td for one reader of schema, written at pointer, keeping
// keepingValue on it. On the schema's own node it keeps, then runs fin, the
// frames outside. On the node standing for st's position it says what it will
// keep, lets fin, the position's finisher among it, write first, and keeps
// after (keepStanding): neither schema can know what the other keeps before it
// has.
func keepAround(loweringContext lowering.Ctx, ts *compile.Types, st stand, td ir.TypeDef, read keySet, held *principalKept,
	schema *statement.Schema, pointer jsontext.Pointer, keepingValue keeping, fin finisher,
) []ir.Diagnostic {
	unmodeled := &td.Common().Unmodeled
	if st.of == nil {
		diags := keepingValue.keep(unmodeled, keepingValue.keys)
		fin.run(td, read, held)
		return diags
	}
	held = held.saying(keepingValue)
	fin.run(td, read, held)
	return keepStanding(loweringContext, ts, allOfEntries(loweringContext, st.of, st.at), held, unmodeled, schema, pointer, keepingValue)
}

// keepStanding keeps keepingValue, what one reader keeps of schema, the inline
// principal of comp written at pointer, on unmodeled, the Unmodeled of the node
// standing for comp's position, which that position has written: nothing where
// it keeps schema whole, the entry holding them, else each keyword as
// heldAgainst places it, one whose own key is free as keepingValue keeps it,
// and one that key holds otherwise under schema's entry's key (keepApart). This
// is the one way a keyword of schema reaches the node once the position has
// written. A discriminator resolves its routes wherever it is kept
// (routesKept).
func keepStanding(loweringContext lowering.Ctx, ts *compile.Types, comp allOfComposition, held *principalKept, unmodeled *ir.Unmodeled,
	schema *statement.Schema, pointer jsontext.Pointer, keepingValue keeping,
) []ir.Diagnostic {
	if held.whole {
		return routesKept(loweringContext, ts, schema, keepingValue.keys, pointer)
	}
	plain, apart := heldAgainst(*unmodeled, comp, schema, pointer, keepingValue.keys)
	diags := keepingValue.keep(unmodeled, plain)
	if slices.Contains(plain, "discriminator") {
		// The discriminator kept whole holds what the position read out of it.
		promotion.DropContained(loweringContext.Promoter(), unmodeled, annotation.EntryKey("", "discriminator"))
	}
	return append(diags, keepApart(loweringContext, ts, comp, held, unmodeled, schema, pointer, apart, keepingValue.reason)...)
}

// heldAgainst splits keys, keywords schema, an entry of comp written at pointer
// that its position lowers from, keeps, by what unmodeled, the Unmodeled the
// position has written, holds: plain where each one's own key is free, apart
// where it holds another value, and neither where it, or the key of an entry
// around schema (aroundOf), holds the same, kept once there. Two values are the
// same where they render to the same JSON, a number's spelling and an array's
// order with them (writtenKeys.note); one that cannot render is plain, its
// keeper's to refuse as written flat.
func heldAgainst(unmodeled ir.Unmodeled, comp allOfComposition, schema *statement.Schema, pointer jsontext.Pointer, keys []string,
) (plain, apart []string) {
	around := aroundOf(unmodeled, comp, pointer)
	for _, key := range keys {
		entry, taken := unmodeled[annotation.EntryKey("", key)]
		if !taken && len(around) == 0 {
			// The path nearly every keyword takes: nothing to render.
			plain = append(plain, key)
			continue
		}
		raw, err := annotation.RawFromNode(keyNode(schema, key))
		switch {
		case err != nil:
			plain = append(plain, key)
		case taken && bytes.Equal(raw, entry.Value), holdsAlike(around, key, raw):
		case !taken:
			plain = append(plain, key)
		default:
			apart = append(apart, key)
		}
	}
	return plain, apart
}

// aroundOf returns the members of what unmodeled holds under the key of each
// entry around the one written at pointer: a wrapper of comp out of whose allOf
// it is flattened, settled before it (outcomeKeeps.lowered). A keyword one of
// them holds alike is that wrapper's writing, the outermost standing.
func aroundOf(unmodeled ir.Unmodeled, comp allOfComposition, pointer jsontext.Pointer) []map[string]ir.RawValue {
	var out []map[string]ir.RawValue
	for _, b := range comp.entries {
		if !b.wrapper || !strings.HasPrefix(string(pointer), string(b.pointer)+"/") {
			continue
		}
		if entry, kept := unmodeled[comp.entryKey(b.pointer)]; kept {
			out = append(out, keptMembers(entry.Value))
		}
	}
	return out
}

// holdsAlike reports whether one of around holds key with the value raw.
func holdsAlike(around []map[string]ir.RawValue, key string, raw ir.RawValue) bool {
	return slices.ContainsFunc(around, func(m map[string]ir.RawValue) bool {
		v, held := m[key]
		return held && bytes.Equal(v, raw)
	})
}

// keptMembers returns the members of raw, a JSON object, by name, each as
// written; nil where raw is no object.
func keptMembers(raw ir.RawValue) map[string]ir.RawValue {
	var out map[string]ir.RawValue
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// besideHeld returns unmodeled with the value the position schema writes under
// each of keys whose own key unmodeled does not hold yet: one a field reads, or
// the inline principal writing it alike keeps after, holds that key all the
// same, so a wrapper around the principal writing it otherwise is kept under
// its own entry's key, the plain key the position's. Where none is, unmodeled
// itself.
func besideHeld(unmodeled ir.Unmodeled, schema *statement.Schema, keys []string) ir.Unmodeled {
	view, copied := unmodeled, false
	for _, key := range keys {
		k := annotation.EntryKey("", key)
		if _, taken := unmodeled[k]; taken || !hasKey(schema, key) {
			continue
		}
		raw, err := annotation.RawFromNode(keyNode(schema, key))
		if err != nil {
			continue
		}
		if !copied {
			view, copied = make(ir.Unmodeled, len(unmodeled)+len(keys)), true
			maps.Copy(view, unmodeled)
		}
		view[k] = ir.UnmodeledEntry{Value: raw}
	}
	return view
}

// keepApart keeps on unmodeled, the Unmodeled of the node standing for the
// position comp is written at, the keywords apart that schema, comp's inline
// principal written at pointer, writes otherwise than the position keeps them:
// under its entry's key, openapi:allOf/<n>, one object holding those and what
// held says readers before kept there, the rest of schema lowered, for the
// reason they share, at the entry, with one report naming apart. A
// discriminator among them resolves its routes, as one kept anywhere does
// (resolveDiscriminatorRoutes).
func keepApart(loweringContext lowering.Ctx, ts *compile.Types, comp allOfComposition, held *principalKept, unmodeled *ir.Unmodeled,
	schema *statement.Schema, pointer jsontext.Pointer, apart []string, reason ir.UnmodeledReason,
) []ir.Diagnostic {
	if len(apart) == 0 {
		return nil
	}
	kept, diags := holdApart(loweringContext, comp, held, unmodeled, schema, pointer, apart, reason)
	if !kept {
		return diags
	}
	// A carrier keeps the key beneath its own scope, so the report names the
	// entry's path, which every such key ends in.
	diags = append(diags, loweringContext.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"the keywords beside %s write %s otherwise than this inline entry, and one Unmodeled entry under a keyword holds "+
			"one value; the entry's are kept verbatim under Unmodeled under the key naming it, %s, the rest of it lowered",
		comp.subject(), strings.Join(apart, ", "), comp.entryPath(pointer)))
	return append(diags, routesKept(loweringContext, ts, schema, apart, pointer)...)
}

// holdApart writes on unmodeled the object the entry's key of schema, comp's
// inline principal written at pointer, holds: the keywords apart and those held
// says were kept there before, as schema writes them, for the reason they
// share. It reports whether it wrote, and held then counts apart among them.
func holdApart(loweringContext lowering.Ctx, comp allOfComposition, held *principalKept, unmodeled *ir.Unmodeled, schema *statement.Schema,
	pointer jsontext.Pointer, apart []string, reason ir.UnmodeledReason,
) (bool, []ir.Diagnostic) {
	all, shared := slices.Concat(held.apart, apart), reason
	if len(held.apart) > 0 {
		shared = sharedOf(held.apartReason, reason)
	}
	members := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, key := range schema.Keys {
		if slices.Contains(all, key.Name) {
			members.Content = append(members.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key.Name}, key.Node)
		}
	}
	kept, diags := annotation.PreserveNodeInto(unmodeled, comp.entryKey(pointer), members, shared, loweringContext.Place().At(pointer))
	if kept {
		held.apart, held.apartReason = all, shared
	}
	return kept, diags
}

// keepBuiltAround finishes td by fin, the frames outside every reader of the
// inline principal schema written at pointer, the position's among them,
// holding back what the principal's build kept on td itself: a keyword kept
// beside the field it could not fill, which no reader keeps after the position.
// Those entries leave the node while the position writes, and are kept after it
// against what it wrote (keepBuilt), so whatever a build keeps reaches the node
// through the one writer too.
func keepBuiltAround(loweringContext lowering.Ctx, st stand, td ir.TypeDef, read keySet, held *principalKept, schema *statement.Schema,
	pointer jsontext.Pointer, fin finisher,
) []ir.Diagnostic {
	unmodeled := &td.Common().Unmodeled
	built := takeBuilt(unmodeled, pointer)
	held = held.saying(keeping{})
	for _, entry := range built {
		held.built = sharedOf(cmp.Or(held.built, entry.Reason), entry.Reason)
	}
	fin.run(td, read, held)
	return keepBuilt(loweringContext, allOfEntries(loweringContext, st.of, st.at), held, unmodeled, schema, pointer, built)
}

// takeBuilt removes from unmodeled, and returns, the entries written at pointer
// or beneath it: what the schema written there kept on the node it built.
func takeBuilt(unmodeled *ir.Unmodeled, pointer jsontext.Pointer) ir.Unmodeled {
	var built ir.Unmodeled
	for key, entry := range *unmodeled {
		at := entry.Provenance.Pointer
		if at != pointer && !strings.HasPrefix(string(at), string(pointer)+"/") {
			continue
		}
		if built == nil {
			built = ir.Unmodeled{}
		}
		built[key] = entry
		delete(*unmodeled, key)
	}
	return built
}

// keepBuilt keeps on unmodeled, which the position has written, the entries
// built, what the build of schema, comp's inline principal written at pointer,
// kept: each under its own key where that is free; once where the key holds the
// same value, or the position keeps schema whole, the entry holding it, or an
// entry around schema holds it alike (aroundOf); and under schema's entry's key
// where the position's keyword holds another (holdApart). The build reported
// each where kept, as written flat, which stays true. A key no keyword of
// schema names cannot be held otherwise.
func keepBuilt(loweringContext lowering.Ctx, comp allOfComposition, held *principalKept, unmodeled *ir.Unmodeled, schema *statement.Schema,
	pointer jsontext.Pointer, built ir.Unmodeled,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	around := aroundOf(*unmodeled, comp, pointer)
	for _, key := range slices.Sorted(maps.Keys(built)) {
		entry := built[key]
		cur, taken := (*unmodeled)[key]
		keyword := strings.TrimPrefix(key, annotation.EntryKey("", ""))
		switch {
		case taken && (held.whole || bytes.Equal(cur.Value, entry.Value)), holdsAlike(around, keyword, entry.Value):
		case !taken:
			annotation.PreserveInto(unmodeled, key, entry.Value, entry.Reason, entry.Provenance)
		case !hasKey(schema, keyword):
			diags = append(diags, loweringContext.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
				"internal: %s, kept by the build of this allOf entry, is held with another value", key))
		default:
			_, heldDiags := holdApart(loweringContext, comp, held, unmodeled, schema, pointer, []string{keyword}, entry.Reason)
			diags = append(diags, heldDiags...)
		}
	}
	if _, kept := built[annotation.EntryKey("", "discriminator")]; kept || held.whole && len(built) > 0 {
		// What the position read out of a discriminator kept whole, and what a
		// whole entry holds, is kept once (dropFoldedIntoWhole).
		promotion.DropContained(loweringContext.Promoter(), unmodeled, annotation.EntryKey("", "discriminator"))
		dropFoldedIntoWhole(loweringContext, unmodeled)
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
// writes where kept, keywords kept another way than keepUnreadKeys keeps them,
// holds it: a discriminator kept verbatim resolves its routes wherever it is
// kept, as keepUnreadKeys resolves the one it keeps (resolveDiscriminatorRoutes).
func routesKept(loweringContext lowering.Ctx, ts *compile.Types, schema *statement.Schema, kept []string, pointer jsontext.Pointer) []ir.Diagnostic {
	if !slices.Contains(kept, "discriminator") {
		return nil
	}
	return resolveDiscriminatorRoutes(loweringContext, ts, schema, pointer)
}

// outcome returns what st's position keeps, having lowered to the node it owns.
func (st stand) outcome(loweringContext lowering.Ctx) outcomeKeeps {
	return keepsAtOutcome(loweringContext, st.of, ir.TypeRef{Target: loweringContext.IDs().ForPointer(st.at)}, st.at)
}

// heldWhole reports whether st's position keeps the inline principal written at
// pointer whole, under its entry's key. A type list asks before it builds its
// variants, which no finisher reaches; every other reader of the principal is
// told (principalKept.whole).
func (st stand) heldWhole(loweringContext lowering.Ctx, pointer jsontext.Pointer) bool {
	return st.of != nil && st.outcome(loweringContext).holdsWhole(pointer)
}

// holdsWhole reports whether k keeps the entry written at pointer whole.
func (k outcomeKeeps) holdsWhole(pointer jsontext.Pointer) bool {
	return slices.ContainsFunc(k.entries, func(keptEntryValue keptEntry) bool {
		return keptEntryValue.entry.pointer == pointer && keptEntryValue.keptWhole()
	})
}
