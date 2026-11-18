// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
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
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// apart returns the keywords of unread, what the inline principal s leaves
// unread on td, the node standing for st's position, that the
// position keeps verbatim too and writes otherwise: one key on one node holds
// one value, so the position's holds it, as its flat spelling's does, and s's is
// kept under its entry's key (keepApart). It is none where s stands for no
// position; a principal the position keeps whole is no caller's to ask, its
// entry's key holding all s writes (heldWhole,
// docs/compilers/openapi.md#allof-inline-residue).
func (st stand) apart(c lowering.Ctx, s *statement.Schema, unread []string, td ir.TypeDef) []string {
	if st.of == nil || len(unread) == 0 {
		return nil
	}
	keeps := st.outcome(c)
	kept := slices.Concat(keeps.keywords, keeps.text)
	if r, refined := refinementOf(c, st.of, st.at); refined {
		if _, set := valueSetPrincipal(r); set {
			// A value set's node keeps the position's own keywords in its
			// build (lowerPrincipalValueSet), which the outcome keeps none of.
			kept = unreadKeys(st.of, valueSetReads(st.of, td)|readBeside(st.of, td)|dispatchOf(st.of).besideAllOf().handled())
		}
	}
	var out []string
	for _, key := range unread {
		if slices.Contains(kept, key) && !keyWrittenAlike(s, st.of, key) {
			out = append(out, key)
		}
	}
	return out
}

// keepTextStanding keeps on sc, a Scalar carrying a number as text, the value
// bounds s, written at pointer, writes, which it holds none of: those the
// position keeps otherwise under s's entry (keepApart), all of them unreported
// where the position keeps s whole (keepHeld), none on a variant of s so held
// (stand.held), and the rest as keepTextBounds keeps them. It returns those
// kept under s's entry.
func keepTextStanding(c lowering.Ctx, st stand, sc *ir.Scalar, s *statement.Schema, pointer jsontext.Pointer,
) ([]string, []ir.Diagnostic) {
	text := declaredConstraints(s)
	switch {
	case st.held:
		// A variant of a principal held whole keeps none, nor reads them: its
		// union's census holds them for the whole entry.
		return nil, nil
	case st.heldWhole(c, pointer):
		keepHeld(c, st, &sc.Unmodeled, s, pointer, text, ir.ReasonNoIRHome)
		return nil, nil
	}
	apart := st.apart(c, s, text, sc)
	diags := keepApart(c, st, &sc.Unmodeled, s, pointer, apart, ir.ReasonNoIRHome)
	return apart, append(diags, keepTextBounds(c, &sc.Unmodeled, s, without(text, apart), pointer)...)
}

// heldByCensus returns the keywords the census of k's inline principal staged
// on p, the node standing for the position, where k keeps that principal
// whole (keepHeld), which the whole entry's report names, in the order the
// principal writes them.
func heldByCensus(k outcomeKeeps, p ir.Unmodeled) []string {
	e := k.principal
	if e == nil || e.schema.Ref != nil || !k.holdsWhole(e.pointer) {
		return nil
	}
	staged := stagedKeys(p, k.comp.branchKey(e.pointer))
	var out []string
	for _, key := range e.schema.Keys {
		if slices.Contains(staged, key.Name) {
			out = append(out, key.Name)
		}
	}
	return out
}

// outcome returns what st's position keeps, having lowered to the node it owns.
func (st stand) outcome(c lowering.Ctx) outcomeKeeps {
	return keepsAtOutcome(c, st.of, ir.TypeRef{Target: c.IDs().ForPointer(st.at)}, st.at)
}

// heldWhole reports whether st's position keeps the inline principal written at
// pointer whole, under its entry's key, which then holds every keyword of it
// that no field reads, kept and reported there once.
func (st stand) heldWhole(c lowering.Ctx, pointer jsontext.Pointer) bool {
	return st.of != nil && st.outcome(c).holdsWhole(pointer)
}

// holdsWhole reports whether k keeps the entry written at pointer whole.
func (k outcomeKeeps) holdsWhole(pointer jsontext.Pointer) bool {
	return slices.ContainsFunc(k.entries, func(e keptEntry) bool { return e.entry.pointer == pointer && e.keptWhole() })
}

// keepHeld stages on p, unreported, keys of s, written at pointer, the inline
// principal st's position keeps whole: what its census leaves unread, which the
// whole entry holds once and its report names (heldByCensus). They wait under
// the entry's key, with any staged before, until the whole entry takes it.
func keepHeld(c lowering.Ctx, st stand, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer, keys []string,
	reason ir.UnmodeledReason,
) {
	key := allOfBranches(c, st.of, st.at).branchKey(pointer)
	held := slices.Concat(stagedKeys(*p, key), keys)
	members := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range s.Keys {
		if slices.Contains(held, k.Name) {
			members.Content = append(members.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k.Name}, k.Node)
		}
	}
	// A value that cannot render fails in the whole entry too, which reports it.
	_, _ = annotation.PreserveNodeInto(p, key, members, reason, c.Place().At(pointer))
}

// stagedKeys returns the members of the object p holds under key, none where it
// holds none or no object.
func stagedKeys(p ir.Unmodeled, key string) []string {
	var members map[string]jsontext.Value
	if e, ok := p[key]; !ok || json.Unmarshal(e.Value, &members) != nil {
		return nil
	}
	return slices.Collect(maps.Keys(members))
}

// keyWrittenAlike reports whether a and b write key with one value, rendered as
// JSON; one that cannot render is alike to nothing.
func keyWrittenAlike(a, b *statement.Schema, key string) bool {
	x, errA := annotation.RawFromNode(keyNode(a, key))
	y, errB := annotation.RawFromNode(keyNode(b, key))
	return errA == nil && errB == nil && bytes.Equal(x, y)
}

// without returns keys less those of drop, in their order.
func without(keys, drop []string) []string {
	return slices.DeleteFunc(slices.Clone(keys), func(key string) bool { return slices.Contains(drop, key) })
}

// keepApart keeps on p, the Unmodeled of the node standing for st's position,
// the keywords apart the inline principal s, written at pointer, writes
// otherwise than the position does (stand.apart): under its entry's key,
// openapi:allOf/<n>, as an object holding them alone, the rest of s lowered,
// for reason, at the entry, with one report there saying why.
func keepApart(c lowering.Ctx, st stand, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer, apart []string,
	reason ir.UnmodeledReason,
) []ir.Diagnostic {
	if len(apart) == 0 {
		return nil
	}
	members := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range s.Keys {
		if slices.Contains(apart, k.Name) {
			members.Content = append(members.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k.Name}, k.Node)
		}
	}
	comp := allOfBranches(c, st.of, st.at)
	key := comp.branchKey(pointer)
	kept, diags := annotation.PreserveNodeInto(p, key, members, reason, c.Place().At(pointer))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"the keywords beside %s write %s otherwise than this inline branch, and one entry under a keyword holds "+
			"one value; the branch's are kept verbatim under Unmodeled under its own key, %s, the rest of it lowered",
		comp.subject(), strings.Join(apart, ", "), key))
}

// recordUnhomedStanding is recordUnhomedAt on td, in hand, for s, written at
// pointer, whose node stands for st's position where st names one: what the
// position keeps otherwise is kept under s's entry (keepApart), and the rest as
// recordUnhomedAt keeps it.
func recordUnhomedStanding(c lowering.Ctx, ts *compile.Types, td ir.TypeDef, s *statement.Schema, unread []string, shape string,
	pointer jsontext.Pointer, st stand,
) []ir.Diagnostic {
	p := &td.Common().Unmodeled
	if st.heldWhole(c, pointer) {
		keepHeld(c, st, p, s, pointer, unread, ir.ReasonDegradedLowering)
		return nil
	}
	apart := st.apart(c, s, unread, td)
	diags := keepApart(c, st, p, s, pointer, apart, ir.ReasonDegradedLowering)
	if rest := without(unread, apart); len(rest) > 0 {
		diags = append(diags, recordUnhomedAt(c, ts, p, s, rest, shape, pointer)...)
	}
	return diags
}
