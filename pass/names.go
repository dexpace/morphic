// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"
	"maps"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var _ Pass = Names{}

// NamesName is the stable name of the [Names] pass.
const NamesName = "names"

// Names reports what a document's names leave to its targets: entities of one
// scope whose names a target may render alike, which it can then tell apart
// only by ID, and a name with no word character, which a target names from its
// characters (docs/ir/naming.md#names-one-scope-shares). It is an analysis, so
// Run hands back the document it was given, and it holds no state, so it is
// safe for concurrent use.
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
// the last, so a cancel waits out one at most. Servers, channels, messages, a
// field's arguments and a resource's identifiers and properties wait for a
// target to name them (GitHub #1413), and a service's renames for a compiler to
// write one (GitHub #1419).
func checkNames(ctx context.Context, doc *ir.Document) ([]ir.Diagnostic, error) {
	if doc == nil {
		return nil, nil
	}
	var nameReaderValue nameReader
	for _, family := range nameReaderValue.families() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		family(doc)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nameReaderValue.diags, nil
}

// families returns the families of scopes r reads, in the order they report.
func (r *nameReader) families() []func(*ir.Document) {
	return []func(*ir.Document){r.services, r.groups, r.headers, r.types, r.auth, r.inherited}
}

// services reads the document's services as one scope.
func (r *nameReader) services(doc *ir.Document) {
	svcs := doc.Services
	if r.readScope(byWords, len(svcs), func(i int) ir.Naming { return svcs[i].Name }) {
		r.report(scope{one: "service", many: "services",
			ids: idsOf(len(svcs), func(i int) string { return string(svcs[i].ID) })})
	}
}

// groups reads each service's top-level groups, and below them each group's
// groups, its operations and each operation's parameters. Groups past
// ir.ForEachGroup's bounds go unread, which validate reports as
// ir/walk-truncated. An operation overloading another (OverloadOf) is compared
// like any other, as no compiler sets one yet.
func (r *nameReader) groups(doc *ir.Document) {
	for _, svc := range doc.Services {
		r.groupList("service", string(svc.ID), svc.Groups)
	}
	ir.ForEachGroup(doc, func(operationGroup ir.OperationGroup) {
		r.groupList("operation group", string(operationGroup.ID), operationGroup.Groups)
		ops := operationGroup.Operations
		if r.readScope(byWords, len(ops), func(i int) ir.Naming { return ops[i].Name }) {
			r.report(scope{in: "operation group", id: string(operationGroup.ID), one: "operation", many: "operations",
				ids: idsOf(len(ops), func(i int) string { return string(ops[i].ID) })})
		}
		for i := range ops {
			r.params(ops[i].ID, ops[i].Params)
			r.choices(ops[i].ID, ops[i].ParamChoices)
		}
	})
}

// groupList reads groups, held by the in named id, as one scope compared
// joined.
func (r *nameReader) groupList(in, id string, groups []ir.OperationGroup) {
	if r.readScope(byJoinedWords, len(groups), func(i int) ir.Naming { return groups[i].Name }) {
		r.report(scope{in: in, id: id, one: "operation group", many: "operation groups",
			ids: idsOf(len(groups), func(i int) string { return string(groups[i].ID) })})
	}
}

// params reads the parameters of the operation op as one scope.
func (r *nameReader) params(op ir.OperationID, params []ir.Parameter) {
	if r.readScope(byWords, len(params), func(i int) ir.Naming { return params[i].Name }) {
		r.report(scope{in: "operation", id: string(op), one: "parameter", many: "parameters",
			ids: idsOf(len(params), func(i int) string { return string(params[i].ID) })})
	}
}

// choices reads the parameter choices of the operation op as one scope, and
// each choice's variants as one of its own. Neither holds an ID, so a party is
// named by its index, and a target tells two named alike apart by position.
func (r *nameReader) choices(op ir.OperationID, choices []ir.ParamChoice) {
	if r.readScope(byWords, len(choices), func(i int) ir.Naming { return choices[i].Name }) {
		r.report(scope{in: "operation", id: string(op), one: "parameter choice", many: "parameter choices",
			apart: "position"})
	}
	for i := range choices {
		variants := choices[i].Variants
		if r.readScope(byWords, len(variants), func(k int) ir.Naming { return variants[k].Name }) {
			r.report(scope{in: "parameter choice " + strconv.Itoa(i) + " of operation", id: string(op),
				one: "variant", many: "variants", apart: "position"})
		}
	}
}

// types reads the parts of every live type, each type's as one scope, and then
// the declared types as one scope of their own, compared joined. An anonymous
// type is left out of that one: a target may inline it rather than name it,
// and its hint is a suggestion, which a target settles against any other name
// by ID. So is a type a target resolves through its type table, which it never
// declares.
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
	if r.read(byJoinedWords) {
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
// or variants, as one scope, a union's variants compared joined. A model's are
// its own properties; inherited reads the rest.
func (r *nameReader) typeParts(id ir.TypeID, td ir.TypeDef) {
	switch t := td.(type) {
	case *ir.Model:
		if r.readScope(byWords, len(t.Properties), func(i int) ir.Naming { return t.Properties[i].Name }) {
			r.report(scope{in: "model", id: string(id), one: "property", many: "properties"})
		}
	case *ir.Enum:
		if r.readScope(byWords, len(t.Members), func(i int) ir.Naming { return t.Members[i].Name }) {
			r.report(scope{in: "enum", id: string(id), one: "member", many: "members"})
		}
	case *ir.Union:
		if r.readScope(byJoinedWords, len(t.Variants), func(i int) ir.Naming { return t.Variants[i].Name }) {
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
	if r.read(byWords) {
		r.report(scope{one: "auth scheme", many: "auth schemes",
			ids: idsOf(len(ids), func(i int) string { return string(ids[i]) })})
	}
}
