package engine

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// danglePass is a test-only transform: it returns a new document carrying a
// dangling type reference and leaves its input alone.
type danglePass struct{}

func (danglePass) Name() string { return "dangle" }

func (danglePass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	next := *doc
	next.Services = []ir.Service{{
		ID: "s/x",
		Groups: []ir.OperationGroup{{
			Operations: []ir.Operation{{
				ID: "op/x",
				Errors: []ir.ErrorCase{{
					Payload: &ir.Payload{Contents: []ir.Content{{Type: ir.TypeRef{Target: "t/missing"}}}},
				}},
			}},
		}},
	}}
	return &next, []ir.Diagnostic{{Code: "test/dangled"}}
}

// nilPass breaks the Pass contract by returning no document.
type nilPass struct{}

func (nilPass) Name() string { return "nil" }

func (nilPass) Run(*ir.Document) (*ir.Document, []ir.Diagnostic) { return nil, nil }

// mutatingPass breaks the Pass contract by writing to its input.
type mutatingPass struct{}

func (mutatingPass) Name() string { return "mutate" }

func (mutatingPass) Run(doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	doc.Name = "mutated"
	return doc, nil
}

func runWith(t *testing.T, passes ...pass.Pass) (*Result, error) {
	t.Helper()
	eng, err := NewWith(openapi.New())
	require.NoError(t, err)
	eng.passes = passes

	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(testspec.Tiny), 0o644))
	return eng.Run(t.Context(), path, RunOptions{})
}

// TestEngine_RunThreadsATransformThroughThePassInterface adds a pass the engine
// has never heard of, with Run's signature untouched, and shows each pass sees
// the previous one's output: order decides whether validate finds the defect.
func TestEngine_RunThreadsATransformThroughThePassInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		passes    []pass.Pass
		wantCodes []string
	}{
		{"transform then validate", []pass.Pass{danglePass{}, pass.NewValidate()},
			[]string{"test/dangled", "ir/dangling-type-ref"}},
		{"validate then transform", []pass.Pass{pass.NewValidate(), danglePass{}},
			[]string{"test/dangled"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := runWith(t, tt.passes...)

			require.NoError(t, err)
			var got []string
			for _, d := range res.Diagnostics {
				got = append(got, d.Code)
			}
			assert.Empty(t, cmp.Diff(tt.wantCodes, got))
			assert.Len(t, res.Document.Services, 1, "the transform's document is the one returned")
		})
	}
}

func TestEngine_RunRefusesAPassReturningNoDocument(t *testing.T) {
	t.Parallel()

	res, err := runWith(t, nilPass{})

	require.ErrorIs(t, err, errNilPassDocument)
	assert.Nil(t, res)
}

// impure reports what a pass changed in its input, or "" when it left it alone.
func impure(t *testing.T, p pass.Pass, doc *ir.Document) string {
	t.Helper()
	before, err := json.Marshal(doc)
	require.NoError(t, err)

	p.Run(doc)

	after, err := json.Marshal(doc)
	require.NoError(t, err)
	return cmp.Diff(string(before), string(after))
}

func TestPass_RunLeavesItsInputUntouched(t *testing.T) {
	t.Parallel()
	res, err := runWith(t)
	require.NoError(t, err)

	for _, p := range []pass.Pass{pass.NewValidate(), danglePass{}} {
		assert.Empty(t, impure(t, p, res.Document), p.Name())
	}
	assert.NotEmpty(t, impure(t, mutatingPass{}, res.Document),
		"the detector must go red on a pass that mutates its input")
}

func TestPass_ValidateNameAndNilInput(t *testing.T) {
	t.Parallel()

	p := pass.NewValidate()
	out, diags := p.Run(nil)

	assert.Equal(t, "validate", p.Name())
	assert.Nil(t, out)
	assert.Empty(t, diags)
}
