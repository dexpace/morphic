package engine_test

import (
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

func (pruneUnused) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
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

func (misusedEdit) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
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
