package graphql

import (
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// lowerService lowers the three root operation types into one Service with a
// query, mutation, and subscription group (ir-design §7.1). GraphQL has no
// service name or core auth concept, so Name and Auth stay empty.
func (l *lowerer) lowerService() ir.Service {
	return ir.Service{
		ID:         serviceID(0),
		Docs:       docsFrom(l.schemaDescription()),
		Groups:     l.operationGroups(),
		Provenance: ir.Provenance{Source: 0},
	}
}

// operationGroups builds one OperationGroup per present, non-empty root type.
func (l *lowerer) operationGroups() []ir.OperationGroup {
	var groups []ir.OperationGroup
	groups = l.appendGroup(groups, "query", l.roots.query)
	groups = l.appendGroup(groups, "mutation", l.roots.mutation)
	groups = l.appendGroup(groups, "subscription", l.roots.subscription)
	return groups
}

// appendGroup appends the operation group for one root type kind, skipping a
// root type that is undeclared or has no fields.
func (l *lowerer) appendGroup(groups []ir.OperationGroup, kind, typeName string) []ir.OperationGroup {
	if typeName == "" {
		return groups
	}
	md, ok := l.defs[typeName]
	if !ok || len(md.def.Fields) == 0 {
		return groups
	}
	return append(groups, ir.OperationGroup{
		Name:       namingFor(kind),
		Docs:       docsFrom(md.def.Description),
		Operations: l.rootOperations(md.def, kind),
		Extensions: lowerDirectives(md.def.Directives),
	})
}

// rootOperations lowers every field of a root type into an operation.
func (l *lowerer) rootOperations(d *ast.Definition, kind string) []ir.Operation {
	ops := make([]ir.Operation, 0, len(d.Fields))
	for _, f := range d.Fields {
		ops = append(ops, l.rootOperation(f, kind))
	}
	return ops
}

// rootOperation lowers one root-type field into an Operation: its arguments
// become Params, its return type becomes the single response, and it binds via
// GraphQLBinding. Query fields are side-effect-free (safe); subscription fields
// carry server-streaming semantics (ir-design §8.4).
func (l *lowerer) rootOperation(f *ast.FieldDefinition, kind string) ir.Operation {
	pointer := opPtr(kind, f.Name)
	op := ir.Operation{
		ID:          opID(pointer),
		Name:        namingFor(f.Name),
		Docs:        docsFrom(f.Description),
		Deprecation: deprecationFrom(f.Directives),
		Params:      l.lowerArgs(f.Arguments, pointer),
		Responses:   l.operationResponses(f, pointer),
		Idempotency: idempotencyFor(kind),
		Bindings:    ir.OpBindings{GraphQL: &ir.GraphQLBinding{Kind: kind, FieldPath: []string{f.Name}}},
		Extensions:  lowerDirectives(f.Directives),
		Provenance:  l.nodeProvenance(pointer, f.Position),
	}
	if kind == "subscription" {
		op.Streaming = ir.StreamingServer
		op.ResponseStream = &ir.StreamDetail{}
	}
	return op
}

// operationResponses builds the single response carrying the field's return
// type. GraphQL has no status codes, so Conditions stay empty; the content is
// media-type-neutral because the GraphQLBinding identifies the protocol.
func (l *lowerer) operationResponses(f *ast.FieldDefinition, pointer string) []ir.Response {
	resultRef := l.typeRef(f.Type, pointer+"/result")
	return []ir.Response{{
		Payload: &ir.Payload{Contents: []ir.Content{{Type: resultRef}}},
	}}
}

// idempotencyFor classifies a root field by kind: query fields are safe (no side
// effects); mutation and subscription fields are left unknown.
func idempotencyFor(kind string) ir.Idempotency {
	if kind == "query" {
		return ir.Idempotency{Kind: ir.IdempotencySafe}
	}
	return ir.Idempotency{}
}

// schemaDescription returns the first non-empty description on a schema block.
func (l *lowerer) schemaDescription() string {
	for _, block := range l.doc.Schema {
		if block.Description != "" {
			return block.Description
		}
	}
	return ""
}
