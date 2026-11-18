// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"encoding/json/jsontext"
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

// apart returns the keywords of unread, what the inline principal s, written
// at pointer, leaves unread on td, the node standing for st's position, that the
// position keeps verbatim too and writes otherwise: one key on one node holds
// one value, so the position's holds it, as its flat spelling's does, and s's is
// kept under its entry's key (keepApart). It is none where s stands for no
// position, and none where the position keeps s whole, its entry's key then
// holding all s writes (docs/compilers/openapi.md#allof-inline-residue).
func (st stand) apart(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer, unread []string, td ir.TypeDef) []string {
	if st.of == nil || len(unread) == 0 {
		return nil
	}
	keeps := keepsAtOutcome(c, st.of, ir.TypeRef{Target: c.IDs().ForPointer(st.at)}, st.at)
	if slices.ContainsFunc(keeps.entries, func(e keptEntry) bool { return e.entry.pointer == pointer && e.keptWhole() }) {
		return nil
	}
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
	apart := st.apart(c, s, pointer, unread, td)
	diags := keepApart(c, st, p, s, pointer, apart, ir.ReasonDegradedLowering)
	if rest := without(unread, apart); len(rest) > 0 {
		diags = append(diags, recordUnhomedAt(c, ts, p, s, rest, shape, pointer)...)
	}
	return diags
}
