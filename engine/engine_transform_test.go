// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine_test

import (
	"context"
	"encoding/json/v2"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/pass"
)

// petsSpec declares a schema its one operation returns and one nothing uses.
const petsSpec = `openapi: 3.1.0
info: {title: Pets, version: "1"}
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Pet"}
components:
  schemas:
    Pet:
      type: object
      properties:
        name: {type: string}
    Orphan:
      type: object
      properties:
        age: {type: integer}
`

// pruneUnused is a filter of the simplest kind: it removes every type nothing
// on the API surface reaches, an irfacts.Usage of 0, through an ir.Editor, so
// it writes no registry of its own.
type pruneUnused struct{}

func (pruneUnused) Name() string { return "prune-unused" }

func (pruneUnused) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	facts := irfacts.New(doc)
	e := ir.NewEditor(doc)
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		if facts.Usage(id) == 0 {
			e.RemoveType(id)
		}
	}
	return e.Finish()
}

// misusedEdit is a transform with a bug: it removes a type the document does
// not hold.
type misusedEdit struct{}

func (misusedEdit) Name() string { return "misused-edit" }

func (misusedEdit) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	e := ir.NewEditor(doc)
	e.RemoveType("t/absent")
	return e.Finish()
}

// holdsType reports whether doc holds a type its source names name.
func holdsType(doc *ir.Document, name string) bool {
	for _, td := range doc.Types {
		if td.Common().Name.Source == name {
			return true
		}
	}
	return false
}

// compiledPets returns petsSpec compiled with no pass run.
func compiledPets(t *testing.T) *ir.Document {
	t.Helper()
	eng, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses())
	require.NoError(t, err)
	res, err := eng.Run(t.Context(), writeSpec(t, petsSpec), engine.RunOptions{})
	require.NoError(t, err)
	require.False(t, res.Failed, "%v", res.Diagnostics)
	return res.Document
}

// TestEngine_RunTransformsThroughTheEditor runs a transform that removes a
// type before validate, as a filter pass would: the run returns a document
// without the unused schema, keeps the used one, and validate finds nothing
// the removal left dangling.
func TestEngine_RunTransformsThroughTheEditor(t *testing.T) {
	t.Parallel()
	control := compiledPets(t)
	require.True(t, holdsType(control, "Orphan"), "the control must hold the schema the transform removes")
	eng, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses(pruneUnused{}, pass.Validator{}))
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, petsSpec), engine.RunOptions{})

	require.NoError(t, err)
	assert.False(t, res.Failed, "%v", res.Diagnostics)
	assert.Empty(t, res.Diagnostics)
	assert.False(t, holdsType(res.Document, "Orphan"), "the unused schema survived the transform")
	assert.True(t, holdsType(res.Document, "Pet"), "the used schema was removed")
	assert.Less(t, len(res.Document.Types), len(control.Types))
}

// TestEngine_RunDocumentKeepsTheCallersDocumentThroughAnEdit pins that a
// transform editing the registry, run over a document the caller holds,
// returns its edit and leaves the caller's document byte for byte as it was.
func TestEngine_RunDocumentKeepsTheCallersDocumentThroughAnEdit(t *testing.T) {
	t.Parallel()
	doc := compiledPets(t)
	before, err := json.Marshal(doc)
	require.NoError(t, err)
	eng, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses(pruneUnused{}))
	require.NoError(t, err)

	res, err := eng.RunDocument(t.Context(), doc, engine.RunOptions{})

	require.NoError(t, err)
	assert.False(t, holdsType(res.Document, "Orphan"))
	after, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "the caller's document changed")
}

// TestEngine_RunFailsOnARefusedEdit pins that a transform's misuse of an editor
// fails the run at the default threshold: the refusal reaches the Result as an
// error, beside the document the transform returned.
func TestEngine_RunFailsOnARefusedEdit(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses(misusedEdit{}))
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, petsSpec), engine.RunOptions{})

	require.NoError(t, err)
	assert.True(t, res.Failed)
	require.NotNil(t, res.Document)
	assert.Equal(t, []string{ir.EditRefused}, codesOf(res))
	assert.Equal(t, ir.SeverityError, res.Diagnostics[0].Severity)
}

// shopSpec declares two tags of operations and an auth scheme, so the
// document it compiles to holds a service, groups, operations and an auth
// scheme to edit.
const shopSpec = `openapi: 3.1.0
info: {title: Shop, version: "1"}
security: [{key: []}]
paths:
  /items:
    get: {operationId: listItems, tags: [items], responses: {"200": {description: ok}}}
    post: {operationId: addItem, tags: [items], responses: {"201": {description: ok}}}
  /orders:
    get: {operationId: listOrders, tags: [orders], responses: {"200": {description: ok}}}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`

// editEveryPart is a transform changing every part of a document through an
// ir.Editor: it removes the first operation and marks every other node it
// reaches, and the document itself, as edited.
type editEveryPart struct{}

func (editEveryPart) Name() string { return "edit-every-part" }

func (editEveryPart) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	e := ir.NewEditor(doc)
	edited := func(docs *ir.Docs) { docs.Summary = "edited" }
	removed := false
	ir.ForEachOperation(doc, func(op ir.Operation) {
		if !removed {
			e.RemoveOperation(op.ID)
			removed = true
			return
		}
		e.UpdateOperation(op.ID, func(op *ir.Operation) { edited(&op.Docs) })
	})
	for _, svc := range doc.Services {
		e.UpdateService(svc.ID, func(s *ir.Service) { edited(&s.Docs) })
		for _, g := range svc.Groups {
			e.UpdateGroup(g.ID, func(g *ir.OperationGroup) { edited(&g.Docs) })
		}
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Servers)) {
		e.UpdateServer(id, func(s *ir.Server) { edited(&s.Description) })
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Auth)) {
		e.UpdateAuthScheme(id, func(a *ir.AuthScheme) { edited(&a.Docs) })
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		e.UpdateType(id, func(td ir.TypeDef) { edited(&td.Common().Docs) })
	}
	e.UpdateDocument(func(d *ir.Document) { edited(&d.Docs) })
	return e.Finish()
}

// removeServers is a transform removing every server, whatever still names it.
type removeServers struct{}

func (removeServers) Name() string { return "remove-servers" }

func (removeServers) Run(_ context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	e := ir.NewEditor(doc)
	for _, id := range slices.Sorted(maps.Keys(doc.Servers)) {
		e.RemoveServer(id)
	}
	return e.Finish()
}

// TestEngine_RunLeavesAServerStillNamedToValidate pins what a removal leaves:
// the edit takes a server the service still names, and validate reports the
// name left dangling.
func TestEngine_RunLeavesAServerStillNamedToValidate(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses(removeServers{}, pass.Validator{}))
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, shopSpec), engine.RunOptions{})

	require.NoError(t, err)
	assert.True(t, res.Failed)
	assert.Contains(t, codesOf(res), "ir/dangling-server-ref")
}

// TestEngine_RunTransformsEveryPartThroughTheEditor runs a transform editing
// every part of a document through an ir.Editor, verified: the run holds the
// transform's input to being left as it was and its output to the IR's
// invariants, validate finds nothing, and the edits reach the result.
func TestEngine_RunTransformsEveryPartThroughTheEditor(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(engine.WithCompilers(openapi.New()), engine.WithPasses(editEveryPart{}, pass.Validator{}))
	require.NoError(t, err)

	res, err := eng.Run(t.Context(), writeSpec(t, shopSpec), engine.RunOptions{Verify: true})

	require.NoError(t, err)
	require.False(t, res.Failed, "%v", res.Diagnostics)
	assert.Empty(t, res.Diagnostics)
	doc := res.Document
	var ops []string
	ir.ForEachOperation(doc, func(op ir.Operation) { ops = append(ops, op.Docs.Summary) })
	assert.Equal(t, []string{"edited", "edited"}, ops, "the first operation was not removed or the rest not edited")
	require.Len(t, doc.Services, 1)
	assert.Equal(t, "edited", doc.Services[0].Docs.Summary)
	require.Len(t, doc.Services[0].Groups, 2)
	for _, g := range doc.Services[0].Groups {
		assert.Equal(t, "edited", g.Docs.Summary, "group %s", g.ID)
	}
	require.NotEmpty(t, doc.Servers)
	for id, s := range doc.Servers {
		assert.Equal(t, "edited", s.Description.Summary, "server %s", id)
	}
	require.Len(t, doc.Auth, 1)
	for id, a := range doc.Auth {
		assert.Equal(t, "edited", a.Docs.Summary, "auth scheme %s", id)
	}
	assert.Equal(t, "edited", doc.Docs.Summary)
}
