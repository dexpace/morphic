// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

var _ Pass = Names{}

// NamesName is the stable name of the [Names] pass.
const NamesName = "names"

// Names reports what a document's names leave to every target alike: entities
// of one scope sharing a neutral name, which a target can tell apart only by
// ID, and a name with no word character, which a target names from its
// characters (docs/ir/naming.md#names-one-scope-shares). It is an analysis, so
// Run hands back the document it was given.
type Names struct{}

// Name returns [NamesName].
func (Names) Name() string { return NamesName }

// Run returns doc unchanged, with what checkNames reports about it, or with
// nothing once ctx is done.
func (Names) Run(ctx context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	diags, err := checkNames(ctx, doc)
	if err != nil {
		return doc, nil
	}
	return doc, diags
}

// checkNames reads every scope whose names a target renders into one
// namespace, a family of scopes at a time, and returns what they report, or
// ctx's error alone once ctx is done. It asks ctx before each family and after
// the last, so a cancel waits out one at most. Servers, channels and messages
// wait for a target to name them (GitHub #1413).
func checkNames(ctx context.Context, doc *ir.Document) ([]ir.Diagnostic, error) {
	if doc == nil {
		return nil, nil
	}
	var r nameReader
	for _, family := range r.families() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		family(doc)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.diags, nil
}

// families returns the families of scopes r reads, in the order they report.
func (r *nameReader) families() []func(*ir.Document) {
	return []func(*ir.Document){r.services, r.groups, r.types, r.auth}
}

// services reads the document's services as one scope.
func (r *nameReader) services(doc *ir.Document) {
	if readScope(r, doc.Services, func(s *ir.Service) ir.Naming { return s.Name }) {
		r.report(scope{one: "service", many: "services",
			ids: idsOf(doc.Services, func(s *ir.Service) string { return string(s.ID) })})
	}
}

// groups reads each service's top-level groups, and below them each group's
// groups, its operations and each operation's parameters. Groups nested past
// ir.MaxGroupDepth go unread, which validate reports as ir/walk-truncated.
func (r *nameReader) groups(doc *ir.Document) {
	for _, svc := range doc.Services {
		r.groupList("service", string(svc.ID), svc.Groups)
	}
	ir.ForEachGroup(doc, func(g ir.OperationGroup) {
		r.groupList("operation group", string(g.ID), g.Groups)
		if readScope(r, g.Operations, func(op *ir.Operation) ir.Naming { return op.Name }) {
			r.report(scope{in: "operation group", id: string(g.ID), one: "operation", many: "operations",
				ids: idsOf(g.Operations, func(op *ir.Operation) string { return string(op.ID) })})
		}
		for i := range g.Operations {
			r.params(&g.Operations[i])
		}
	})
}

// groupList reads groups, held by the in named id, as one scope.
func (r *nameReader) groupList(in, id string, groups []ir.OperationGroup) {
	if readScope(r, groups, func(g *ir.OperationGroup) ir.Naming { return g.Name }) {
		r.report(scope{in: in, id: id, one: "operation group", many: "operation groups",
			ids: idsOf(groups, func(g *ir.OperationGroup) string { return string(g.ID) })})
	}
}

// params reads op's parameters as one scope.
func (r *nameReader) params(op *ir.Operation) {
	if readScope(r, op.Params, func(p *ir.Parameter) ir.Naming { return p.Name }) {
		r.report(scope{in: "operation", id: string(op.ID), one: "parameter", many: "parameters"})
	}
}

// types reads the parts of every live type, each type's as one scope, and then
// the declared types as one scope of their own. An anonymous type is left out
// of that one: a target may inline it rather than name it, and its hint is a
// suggestion, which a target settles against any other name by ID. So is a
// type a target resolves through its type table, which it never declares.
func (r *nameReader) types(doc *ir.Document) {
	ids := liveTypeIDs(doc)
	for _, id := range ids {
		r.typeParts(id, doc.Types[id])
	}
	declared := ids[:0]
	r.parties = r.parties[:0]
	for _, id := range ids {
		td := doc.Types[id]
		if td.Common().Anonymous || builtIn(td) {
			continue
		}
		declared = append(declared, id)
		r.parties = append(r.parties, partyOf(td.Common().Name))
	}
	if r.read() {
		r.report(scope{one: "type", many: "types",
			ids: idsOf(declared, func(id *ir.TypeID) string { return string(*id) })})
	}
}

// builtIn reports whether td is a type a target resolves through its type
// table, by its primitive or identity, rather than declares: a primitive, an
// any node or an external (docs/emitters/contract/type-table.md).
func builtIn(td ir.TypeDef) bool {
	switch td.(type) {
	case *ir.Primitive, *ir.Any, *ir.External:
		return true
	default:
		return false
	}
}

// typeParts reads the named parts of the type at id, its properties, members
// or variants, as one scope. A model's are its own properties: one sharing a
// name with a property it inherits goes unreported (GitHub #1412).
func (r *nameReader) typeParts(id ir.TypeID, td ir.TypeDef) {
	switch t := td.(type) {
	case *ir.Model:
		if readScope(r, t.Properties, func(p *ir.Property) ir.Naming { return p.Name }) {
			r.report(scope{in: "model", id: string(id), one: "property", many: "properties"})
		}
	case *ir.Enum:
		if readScope(r, t.Members, func(m *ir.EnumMember) ir.Naming { return m.Name }) {
			r.report(scope{in: "enum", id: string(id), one: "member", many: "members"})
		}
	case *ir.Union:
		if readScope(r, t.Variants, func(v *ir.Variant) ir.Naming { return v.Name }) {
			r.report(scope{in: "union", id: string(id), one: "variant", many: "variants"})
		}
	default:
		// No other kind names its parts.
	}
}

// auth reads the document's auth schemes as one scope, in ID order.
func (r *nameReader) auth(doc *ir.Document) {
	ids := slices.Sorted(maps.Keys(doc.Auth))
	r.parties = r.parties[:0]
	for _, id := range ids {
		r.parties = append(r.parties, partyOf(doc.Auth[id].Name))
	}
	if r.read() {
		r.report(scope{one: "auth scheme", many: "auth schemes",
			ids: idsOf(ids, func(id *ir.AuthID) string { return string(*id) })})
	}
}
