// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package paramchoice

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// PromoteChoices fills each operation's ParamChoices from the key the policy's
// sources of lowering.TargetParamChoices name: an object from a choice's name
// to its optional flag and its variants, each a list of parameter names. A
// choice is filled whole or not at all, one written badly leaving the others,
// and is reported as info diag.UnpromotedExtension at the most specific
// coordinate. An operation holding a filled choice is marked; the key stays.
func PromoteChoices(c lowering.Ctx, w *whole.Document) []ir.Diagnostic {
	p := c.Promoter()
	var diags []ir.Diagnostic
	for _, op := range w.Ops {
		st, written, readDiags := p.ParamChoiceStatement(op.Unmodeled)
		diags = append(diags, readDiags...)
		if !written {
			continue
		}
		var groups map[string]jsontext.Value
		if err := json.Unmarshal(st.Value, &groups); err != nil || groups == nil {
			diags = append(diags, lowering.Unpromoted(st.Source.String(), st.At,
				"is "+lowering.ShapeOf(st.Value)+", not an object of named choices", "no choice is filled"))
			continue
		}
		r := reader{w: w, op: op, key: st.Source.String()}
		for _, name := range slices.Sorted(maps.Keys(groups)) {
			choice, report, ok := r.choice(name, groups[name], at(st.At, name))
			if !ok {
				diags = append(diags, report)
				continue
			}
			op.ParamChoices = append(op.ParamChoices, choice)
		}
		if len(op.ParamChoices) > 0 {
			op.Provenance.Inferred = ir.JoinInferred(op.Provenance.Inferred, lowering.ExtensionPromotionHeuristic)
		}
	}
	return diags
}

// reader reads the choices one operation's key states.
type reader struct {
	w   *whole.Document
	op  *ir.Operation
	key string
}

// refusal is why a choice is not filled, and where that is written.
type refusal struct {
	why string
	at  ir.Provenance
}

// choice reads the choice name, whose value is raw, written at where, and
// returns it, or the report saying why it is not filled.
func (r reader) choice(name string, raw jsontext.Value, where ir.Provenance) (ir.ParamChoice, ir.Diagnostic, bool) {
	choice, no := r.readChoice(name, raw, where)
	if no != nil {
		return ir.ParamChoice{}, lowering.Unpromoted(r.key, no.at, no.why,
			"the choice "+strconv.Quote(name)+" is not filled"), false
	}
	return choice, ir.Diagnostic{}, true
}

// readChoice reads the choice name: an object stating a boolean optional and two
// variants or more, each naming parameters, of which none every request
// carries sits in some variants only.
func (r reader) readChoice(name string, raw jsontext.Value, where ir.Provenance) (ir.ParamChoice, *refusal) {
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return ir.ParamChoice{}, &refusal{"holds " + lowering.ShapeOf(raw) + " for choice " + strconv.Quote(name) +
			", not an object", where}
	}
	optional, no := flag(name, fields, where)
	if no != nil {
		return ir.ParamChoice{}, no
	}
	variants, no := r.variants(name, fields, where)
	if no != nil {
		return ir.ParamChoice{}, no
	}
	if no := r.carriedBySome(variants); no != "" {
		return ir.ParamChoice{}, &refusal{"names parameter " + strconv.Quote(no) +
			", which every request carries, in some variants only", where}
	}
	return ir.ParamChoice{Name: ir.NamingFor(name), Required: !optional, Variants: variants}, nil
}

// flag reads the choice name's optional member, which fields hold.
func flag(name string, fields map[string]jsontext.Value, where ir.Provenance) (bool, *refusal) {
	why := "states no boolean \"optional\" for choice " + strconv.Quote(name)
	raw, present := fields["optional"]
	if !present {
		return false, &refusal{why, where}
	}
	var optional *bool
	if err := json.Unmarshal(raw, &optional); err != nil || optional == nil {
		return false, &refusal{why, at(where, "optional")}
	}
	return *optional, nil
}

// variants reads the choice name's variants, which fields hold: two or more,
// by name, each naming the parameters it may carry.
func (r reader) variants(name string, fields map[string]jsontext.Value, where ir.Provenance) ([]ir.ParamVariant, *refusal) {
	why := "states fewer than two variants for choice " + strconv.Quote(name)
	raw, present := fields["variants"]
	if !present {
		return nil, &refusal{why, where}
	}
	where = at(where, "variants")
	var byName map[string]jsontext.Value
	if err := json.Unmarshal(raw, &byName); err != nil || len(byName) < 2 {
		return nil, &refusal{why, where}
	}
	out := make([]ir.ParamVariant, 0, len(byName))
	for _, variant := range slices.Sorted(maps.Keys(byName)) {
		params, no := r.variantParams(variant, byName[variant], at(where, variant))
		if no != nil {
			return nil, no
		}
		out = append(out, ir.ParamVariant{Name: ir.NamingFor(variant), Params: params})
	}
	return out, nil
}

// variantParams resolves the parameter names the variant holds, a list of one name
// or more, each naming one parameter of the operation and each once.
func (r reader) variantParams(variant string, raw jsontext.Value, where ir.Provenance) ([]ir.ParamID, *refusal) {
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil || len(names) == 0 {
		return nil, &refusal{"names no parameter in variant " + strconv.Quote(variant), where}
	}
	index := r.w.Params(r.op)
	out := make([]ir.ParamID, 0, len(names))
	seen := make(map[ir.ParamID]bool, len(names))
	for k, name := range names {
		id, matched := index.Named(name)
		pos := at(where, strconv.Itoa(k))
		switch {
		case matched == 0:
			return nil, &refusal{"names parameter " + strconv.Quote(name) + ", which the operation does not declare", pos}
		case matched > 1:
			return nil, &refusal{"names parameter " + strconv.Quote(name) + ", which the operation declares in " +
				strconv.Itoa(matched) + " locations", pos}
		case seen[id]:
			return nil, &refusal{"names parameter " + strconv.Quote(name) + " twice in variant " + strconv.Quote(variant), pos}
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// carriedBySome returns the name of a parameter every request carries that
// some of variants name and some do not, or "" where none does: the choice
// would be false of a request picking a variant lacking it.
func (r reader) carriedBySome(variants []ir.ParamVariant) string {
	for _, p := range r.op.Params {
		if !p.Required {
			continue
		}
		in := 0
		for _, v := range variants {
			if slices.Contains(v.Params, p.ID) {
				in++
			}
		}
		if in > 0 && in < len(variants) {
			return p.Name.Source
		}
	}
	return ""
}

// at is where, one token deeper.
func at(where ir.Provenance, token string) ir.Provenance {
	where.Pointer = where.Pointer.AppendToken(token)
	return where
}
