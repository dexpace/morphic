// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"context"
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// members is the run's record of the models with members to settle, each with
// the fields that built it, and which of them settleMembers has read. The
// walks and the folds fill it; settleMembers alone reads it.
type members struct {
	byID    map[ir.TypeID]modelFields
	settled map[ir.TypeID]bool
	// tables holds the Table of each model settled, and of each model a part
	// names that the walks did not note, which composes nothing.
	tables map[ir.TypeID]merge.RedeclaredWireNameTable
	// shared is the wire names more than one model declares (sharedNames).
	shared map[string]bool
	// carried is the notes each property lowered carries off the node its
	// schema built, by that node.
	carried map[ir.TypeID]merge.CarriedNotes
	// reads is what remains of the declarations settleMembers reads
	// (maxMemberReads), and cut the first model it read none for.
	reads budget.Budget
	cut   ir.TypeID
}

// membersOf returns the run's record of models with members to settle.
func membersOf(loweringContext lowering.Ctx) *members {
	return lowering.Settled(loweringContext, func() *members {
		return &members{byID: map[ir.TypeID]modelFields{}, settled: map[ir.TypeID]bool{},
			tables: map[ir.TypeID]merge.RedeclaredWireNameTable{}, carried: map[ir.TypeID]merge.CarriedNotes{},
			reads: budget.New(budget.AtMost(maxMemberReads))}
	})
}

// MembersDeclaredAgain returns the members of the models the run lowered that
// one model declares more than once itself, once its members have settled
// (merge.ModelPropertyDeclarations.DeclaredAgain): the notes of each
// declaration stay on the node its schema builds, not on the member
// (notesOnCarrier), which a reader of the finished document cannot tell from
// the nodes (replacement.Resolve).
func MembersDeclaredAgain(loweringContext lowering.Ctx) map[ir.PropertyID]bool {
	out := map[ir.PropertyID]bool{}
	for _, modelFieldsValue := range membersOf(loweringContext).byID {
		for _, id := range modelFieldsValue.idx.DeclaredAgain() {
			out[id] = true
		}
	}
	return out
}

// note records the model modelFieldsValue builds as one with members to settle,
// and queues the settlement at the first model noted (settleMembers). The first
// fields to note a model place what is minted and reported for it.
func (rec *members) note(modelFieldsValue modelFields) {
	if len(rec.byID) == 0 {
		modelFieldsValue.ts.QueueSettlementBeforeDeferredOnes(settleMembers{c: modelFieldsValue.c})
	}
	if _, ok := rec.byID[modelFieldsValue.m.ID]; !ok {
		rec.byID[modelFieldsValue.m.ID] = modelFieldsValue
	}
}

// settleMembers is the settlement folding, on each model the walk notes, each
// member more than one declaration decides, its own or those its entries bring
// (merge.ModelPropertyDeclarations.Settle;
// docs/compilers/openapi.md#allof-redeclared-property). It is a completion
// (compile.Types.QueueSettlementBeforeDeferredOnes), so it reads every node the
// walks built and every other settlement reads what it writes.
type settleMembers struct {
	c lowering.Ctx
}

// maxMemberRounds bounds the rounds Settle reads the record in. A round
// settles every model noted before it; a model a fold composes is noted during
// one and read in the next, so each round reads a fold nested one level deeper.
const maxMemberRounds = 8

// maxMemberAliasHops bounds how many alias scalars composed and modelOf read a
// part through (styleguide bounded-everything rule), as maxTagAliasHops bounds
// a tag's. A longer chain, or a loop, stops on an alias, which has no members,
// though the flattening reads on (GitHub #1994).
const maxMemberAliasHops = 64

// maxMemberReads bounds the declarations the settlement reads of what the
// models a model composes say (merge.RedeclaredWireNameTable), summed over the
// run (styleguide bounded-everything rule). A table holds only the names more
// than one model declares, so a hierarchy costs its depth again at each level
// only where every level adds such a name, or composes a model redeclaring one
// it holds, whose declaration may stand within the hierarchy
// (merge.Merger.Reads). A read of a model not settled yet, as a cycle's are,
// counts the composition its flattening walks too (chargeComposition). Past the
// bound each model left settles its own declarations alone.
const maxMemberReads = 1 << 24

// Settle settles each noted model, saying once a conflict every model composing
// its two declarations draws alike (merge.ReportOnce).
func (k settleMembers) Settle(ctx context.Context, reg ir.TypeRegistry) []ir.Diagnostic {
	return merge.ReportOnce(k.inRounds(ctx, reg))
}

// inRounds settles each noted model in rounds, each round in settleOrder, so
// every model settles after each noted model it composes, however deep a
// hierarchy nests, and the order models are declared in decides nothing. It
// asks ctx before each model, as one fold may judge 64 declarations pair by
// pair, and returns once it is done: the run then assembles no document.
func (k settleMembers) inRounds(ctx context.Context, reg ir.TypeRegistry) []ir.Diagnostic {
	rec := membersOf(k.c)
	rec.shared = sharedNames(reg)
	sources := k.c.Sources()
	var diags []ir.Diagnostic
	for range maxMemberRounds {
		pending := rec.pending()
		if len(pending) == 0 {
			return append(diags, rec.budgetCut()...)
		}
		for _, id := range rec.settleOrder(reg, pending) {
			if ctx.Err() != nil {
				return diags
			}
			diags = append(diags, rec.settle(reg, id, sources)...)
		}
	}
	if pending := rec.pending(); len(pending) > 0 {
		first := rec.byID[pending[0]]
		diags = append(diags, first.c.DiagAt(ir.SeverityError, diag.BudgetExceeded, first.pointer,
			"models composing what a fold composes nest past the %d rounds their members are settled in; %d models "+
				"are left as their declarations stand", maxMemberRounds, len(pending)))
	}
	return append(diags, rec.budgetCut()...)
}

// budgetCut reports, at the first model the settlement read nothing for, that
// the reads ran out (maxMemberReads), or nothing where they did not.
func (rec *members) budgetCut() []ir.Diagnostic {
	if rec.cut == "" {
		return nil
	}
	modelFields := rec.byID[rec.cut]
	return []ir.Diagnostic{modelFields.c.DiagAt(ir.SeverityError, diag.BudgetExceeded, modelFields.pointer,
		"the settlement read past %d declarations and composition entries at this model; it and each model "+
			"settled after it fold only their own declarations, not those their entries bring", maxMemberReads)}
}

// pending returns the noted models not yet read, by ID.
func (rec *members) pending() []ir.TypeID {
	return slices.DeleteFunc(slices.Sorted(maps.Keys(rec.byID)), func(id ir.TypeID) bool { return rec.settled[id] })
}

// settleFrame is one model on settleOrder's path, and the parts it has yet to
// visit.
type settleFrame struct {
	id    ir.TypeID
	parts []ir.TypeID
}

// settleOrder returns pending, the noted models a round reads, each after every
// pending model it composes: a depth-first walk from each in turn, by ID, that
// lists a model once its parts are listed, run on an explicit stack, as a
// hierarchy may nest as deep as a document is long. A part already on the path
// closes a cycle of compositions, so the model naming it reads it as it
// stands; which model of a cycle settles first follows their IDs, not the
// order the source declares them in (GitHub #1995).
func (rec *members) settleOrder(reg ir.TypeRegistry, pending []ir.TypeID) []ir.TypeID {
	order := make([]ir.TypeID, 0, len(pending))
	seen := make(map[ir.TypeID]bool, len(pending))
	for _, root := range pending {
		if seen[root] {
			continue
		}
		seen[root] = true
		path := []settleFrame{{id: root, parts: rec.composed(reg, root)}}
		for len(path) > 0 {
			top := &path[len(path)-1]
			if len(top.parts) == 0 {
				order = append(order, top.id)
				path = path[:len(path)-1]
				continue
			}
			part := top.parts[0]
			top.parts = top.parts[1:]
			if !seen[part] && !rec.settled[part] {
				seen[part] = true
				path = append(path, settleFrame{id: part, parts: rec.composed(reg, part)})
			}
		}
	}
	return order
}

// settle settles the model id names, reading each model it composes as it
// stands, which settleOrder puts first. A tag reading reads the members it
// settles (tagReadings), so one stored before it is a compiler bug, reported at
// the model.
func (rec *members) settle(reg ir.TypeRegistry, id ir.TypeID, sources []ir.SourceInfo) []ir.Diagnostic {
	rec.settled[id] = true
	modelFields := rec.byID[id]
	merger := merge.Merger{Reg: reg, IDs: modelFields.c.IDs(), Doc: modelFields.c.Place(), Sources: sources, Reads: &rec.reads, Walk: modelFields, Carried: rec.carried,
		Promotion: promotion.ExtensionPromotionHeuristic}
	diags, table := modelFields.idx.Settle(merger, rec.partTables(reg, modelFields.m), rec.shared)
	rec.tables[id] = table
	rec.cutAt(id)
	if readingsOf(modelFields.c).stored() {
		diags = append(diags, modelFields.c.DiagAt(ir.SeverityError, diag.InternalInvariant, modelFields.pointer,
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
		for range maxMemberAliasHops {
			if _, noted := rec.byID[part]; noted {
				out = append(out, part)
				break
			}
			scalar, isAlias := reg[part].(*ir.Scalar)
			if !isAlias || scalar.Base == nil {
				break
			}
			part = scalar.Base.Target
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// partTables returns the Table of each type model composes (merge.ComposedOf),
// read for model (tableOf).
func (rec *members) partTables(reg ir.TypeRegistry, model *ir.Model) map[ir.TypeID]merge.RedeclaredWireNameTable {
	out := map[ir.TypeID]merge.RedeclaredWireNameTable{}
	for _, target := range merge.ComposedOf(model) {
		out[target] = rec.tableOf(reg, target, model.ID)
	}
	return out
}

// tableOf returns the table of the model target names through alias scalars,
// charged to the run's reads: the one its settlement left, or, for a model
// not settled yet, which a cycle reaches or the walks did not note, what its
// flattening takes as it stands but from's (asItStands). A model the walks did
// not note composes nothing, so its table is kept. It returns none once the
// reads run out, and none for a type naming no model.
func (rec *members) tableOf(reg ir.TypeRegistry, target, from ir.TypeID) merge.RedeclaredWireNameTable {
	id, isModel := modelOf(reg, target)
	if !isModel || rec.reads.Exhausted() {
		rec.cutAt(from)
		return nil
	}
	table, kept := rec.tables[id]
	if !kept {
		table = rec.asItStands(reg, id, from)
		if _, noted := rec.byID[id]; !noted {
			rec.tables[id] = table
		}
	}
	if !rec.reads.Spend(len(table)) {
		rec.cutAt(from)
		return nil
	}
	return table
}

// cutAt records from as the first model the settlement read nothing for, where
// the reads have run out and none is recorded yet.
func (rec *members) cutAt(from ir.TypeID) {
	if rec.cut == "" && rec.reads.Exhausted() {
		rec.cut = from
	}
}

// modelOf returns the model target names through alias scalars, and false
// where it names none within maxMemberAliasHops.
func modelOf(reg ir.TypeRegistry, target ir.TypeID) (ir.TypeID, bool) {
	for range maxMemberAliasHops {
		switch td := reg[target].(type) {
		case *ir.Model:
			return target, true
		case *ir.Scalar:
			if td.Base == nil {
				return "", false
			}
			target = td.Base.Target
		default:
			return "", false
		}
	}
	return "", false
}

// asItStands returns the table of the model id as reg holds it: for each name
// more than one model declares, the declaration its flattening takes but
// from's (merge.TakenOutside), charging what the flattening walks and reads.
// It returns none once the reads run out.
func (rec *members) asItStands(reg ir.TypeRegistry, id, from ir.TypeID) merge.RedeclaredWireNameTable {
	if !rec.chargeComposition(reg, id) {
		return nil
	}
	table := merge.RedeclaredWireNameTable{}
	for _, member := range ir.FlattenedProperties(reg, id).Properties {
		rec.reads.Spend(1 + len(member.Overridden))
		if propertyDecl, ok := merge.TakenOutside(member, from); ok && rec.shared[member.Property.WireName] {
			table[member.Property.WireName] = rec.declared(propertyDecl)
		}
	}
	return table
}

// declared is propertyDecl, a declaration a flattening takes, with where its
// type may be written: where the model declaring it says, once settled, or at
// propertyDecl itself.
func (rec *members) declared(propertyDecl ir.PropertyDecl) merge.Declared {
	if settled, ok := rec.tables[propertyDecl.Owner][propertyDecl.Property.WireName]; ok && settled.Property.ID == propertyDecl.Property.ID {
		return settled
	}
	return merge.DeclaredAt(propertyDecl)
}

// chargeComposition spends, of the run's reads, what a flattening of id walks
// besides the declarations it reads: each type its composition reaches
// through Base, Mixins and alias scalars, and each entry naming one. A model
// on a cycle is flattened at each read until it settles, and its composition
// may hold every other model of the cycle. It reports false once the reads
// run out.
func (rec *members) chargeComposition(reg ir.TypeRegistry, id ir.TypeID) bool {
	seen := map[ir.TypeID]bool{id: true}
	for queue := []ir.TypeID{id}; len(queue) > 0; {
		parts := merge.ComposedOf(reg[queue[len(queue)-1]])
		queue = queue[:len(queue)-1]
		if !rec.reads.Spend(1 + len(parts)) {
			return false
		}
		for _, part := range parts {
			if !seen[part] {
				seen[part] = true
				queue = append(queue, part)
			}
		}
	}
	return true
}

// sharedNames returns the wire names more than one model in reg declares, each
// model counted once: a name one model alone declares decides no member twice.
func sharedNames(reg ir.TypeRegistry) map[string]bool {
	first, shared := map[string]ir.TypeID{}, map[string]bool{}
	for id, td := range reg {
		model, isModel := td.(*ir.Model)
		if !isModel {
			continue
		}
		for _, property := range model.Properties {
			switch owner, seen := first[property.WireName]; {
			case !seen:
				first[property.WireName] = id
			case owner != id:
				shared[property.WireName] = true
			}
		}
	}
	return shared
}
