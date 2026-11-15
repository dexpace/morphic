// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/ir"
)

// members is the run's record of the models with members to settle, each with
// the fields that built it, and how far settleMembers has read each. The walks
// and the folds fill it; settleMembers alone reads it.
type members struct {
	byID  map[ir.TypeID]modelFields
	state map[ir.TypeID]memberState
}

// memberState is how far settleMembers has read one model.
type memberState uint8

const (
	memberPending memberState = iota
	memberReading
	memberSettled
)

// membersOf returns the run's record of models with members to settle.
func membersOf(c lowering.Ctx) *members {
	return lowering.Settled(c, func() *members {
		return &members{byID: map[ir.TypeID]modelFields{}, state: map[ir.TypeID]memberState{}}
	})
}

// note records the model f builds as one with members to settle, and queues
// the settlement at the first model noted (settleMembers). The first fields to
// note a model place what is minted and reported for it.
func (rec *members) note(f modelFields) {
	if len(rec.byID) == 0 {
		f.ts.Complete(settleMembers{c: f.c})
	}
	if _, ok := rec.byID[f.m.ID]; !ok {
		rec.byID[f.m.ID] = f
	}
}

// settleMembers is the settlement folding, on each model the walk notes, each
// member more than one declaration decides, its own or those its entries bring
// (merge.PropIndex.Settle; docs/compilers/openapi.md#allof-redeclared-property).
// It is a completion (compile.Types.Complete), so it reads every node the walks
// built and every other settlement reads what it writes.
type settleMembers struct {
	c lowering.Ctx
}

// maxMemberRounds bounds the rounds Settle reads the record in. A round
// settles every model noted before it; a model a fold composes is noted during
// one and read in the next, so each round reads a fold nested one level deeper.
const maxMemberRounds = 8

// maxMemberDepth bounds how many models Settle settles beneath one before it,
// those it composes, each before the model composing it (styleguide
// bounded-recursion rule); past it a model is read as it stands.
const maxMemberDepth = 64

// Settle settles each noted model, by ID, in rounds, each model after those it
// composes, so the order models are declared in decides nothing. A cycle of
// compositions reads the model it returns to as it stands, the same whichever
// is noted first.
func (k settleMembers) Settle(reg ir.TypeRegistry) []ir.Diagnostic {
	rec := membersOf(k.c)
	var diags []ir.Diagnostic
	for range maxMemberRounds {
		pending := rec.pending()
		if len(pending) == 0 {
			return diags
		}
		for _, id := range pending {
			diags = append(diags, rec.settle(reg, id, 0)...)
		}
	}
	if pending := rec.pending(); len(pending) > 0 {
		first := rec.byID[pending[0]]
		diags = append(diags, first.c.DiagAt(ir.SeverityError, diag.BudgetExceeded, first.pointer,
			"models composing what a fold composes nest past the %d rounds their members are settled in; %d models "+
				"are left as their declarations stand", maxMemberRounds, len(pending)))
	}
	return diags
}

// pending returns the noted models not yet read, by ID.
func (rec *members) pending() []ir.TypeID {
	return slices.DeleteFunc(slices.Sorted(maps.Keys(rec.byID)), func(id ir.TypeID) bool {
		return rec.state[id] != memberPending
	})
}

// settle settles the model id names, once, after each noted model it composes,
// read by ID. A tag reading reads the members it settles (tagReadings), so one
// stored before it is a compiler bug, reported at the model.
func (rec *members) settle(reg ir.TypeRegistry, id ir.TypeID, depth int) []ir.Diagnostic {
	if rec.state[id] != memberPending {
		return nil
	}
	rec.state[id] = memberReading
	var diags []ir.Diagnostic
	if depth < maxMemberDepth {
		for _, part := range rec.composed(reg, id) {
			diags = append(diags, rec.settle(reg, part, depth+1)...)
		}
	}
	rec.state[id] = memberSettled
	f := rec.byID[id]
	diags = append(diags, f.idx.Settle(merge.Merger{Reg: reg, IDs: f.c.IDs(), Doc: f.c.Place(), Walk: f})...)
	if readingsOf(f.c).stored() {
		diags = append(diags, f.c.DiagAt(ir.SeverityError, diag.InternalInvariant, f.pointer,
			"internal: the model's members settle after a discriminator-inference reading is stored, which may "+
				"have read them unsettled"))
	}
	return diags
}

// composed returns the noted models id's composition reaches through alias
// scalars, by ID.
func (rec *members) composed(reg ir.TypeRegistry, id ir.TypeID) []ir.TypeID {
	var out []ir.TypeID
	for _, part := range merge.ComposedOf(rec.byID[id].m) {
		for range maxMemberDepth {
			if _, noted := rec.byID[part]; noted {
				out = append(out, part)
				break
			}
			s, isAlias := reg[part].(*ir.Scalar)
			if !isAlias || s.Base == nil {
				break
			}
			part = s.Base.Target
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
