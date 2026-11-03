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
	svcs := doc.Services
	if r.readScope(len(svcs), func(i int) ir.Naming { return svcs[i].Name }) {
		r.report(scope{one: "service", many: "services",
			ids: idsOf(len(svcs), func(i int) string { return string(svcs[i].ID) })})
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
		ops := g.Operations
		if r.readScope(len(ops), func(i int) ir.Naming { return ops[i].Name }) {
			r.report(scope{in: "operation group", id: string(g.ID), one: "operation", many: "operations",
				ids: idsOf(len(ops), func(i int) string { return string(ops[i].ID) })})
		}
		for i := range ops {
			r.params(ops[i].ID, ops[i].Params)
		}
	})
}

// groupList reads groups, held by the in named id, as one scope.
func (r *nameReader) groupList(in, id string, groups []ir.OperationGroup) {
	if r.readScope(len(groups), func(i int) ir.Naming { return groups[i].Name }) {
		r.report(scope{in: in, id: id, one: "operation group", many: "operation groups",
			ids: idsOf(len(groups), func(i int) string { return string(groups[i].ID) })})
	}
}

// params reads the parameters of the operation op as one scope.
func (r *nameReader) params(op ir.OpID, params []ir.Parameter) {
	if r.readScope(len(params), func(i int) ir.Naming { return params[i].Name }) {
		r.report(scope{in: "operation", id: string(op), one: "parameter", many: "parameters",
			ids: idsOf(len(params), func(i int) string { return string(params[i].ID) })})
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
			ids: idsOf(len(declared), func(i int) string { return string(declared[i]) })})
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
		if r.readScope(len(t.Properties), func(i int) ir.Naming { return t.Properties[i].Name }) {
			r.report(scope{in: "model", id: string(id), one: "property", many: "properties",
				ids: idsOf(len(t.Properties), func(i int) string { return string(t.Properties[i].ID) })})
		}
	case *ir.Enum:
		if r.readScope(len(t.Members), func(i int) ir.Naming { return t.Members[i].Name }) {
			r.report(scope{in: "enum", id: string(id), one: "member", many: "members",
				ids: idsOf(len(t.Members), func(i int) string { return string(t.Members[i].ID) })})
		}
	case *ir.Union:
		if r.readScope(len(t.Variants), func(i int) ir.Naming { return t.Variants[i].Name }) {
			r.report(scope{in: "union", id: string(id), one: "variant", many: "variants",
				ids: idsOf(len(t.Variants), func(i int) string { return string(t.Variants[i].Type.Target) })})
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
			ids: idsOf(len(ids), func(i int) string { return string(ids[i]) })})
	}
}
