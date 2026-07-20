package openapi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/ir"
)

// sourceOf wraps a spec string as a frontend.Source.
func sourceOf(src string) frontend.Source {
	return frontend.Source{Path: "spec.yaml", Data: []byte(src)}
}

// parseFull runs the whole public frontend pipeline over src.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := New().Parse(context.Background(),
		[]frontend.Source{{Path: "spec.yaml", Data: []byte(src)}}, frontend.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// findOp returns the operation whose source name matches.
func findOp(t *testing.T, doc *ir.Document, source string) ir.Operation {
	t.Helper()
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if op.Name.Source == source {
				return op
			}
		}
	}
	t.Fatalf("operation %q not found", source)
	return ir.Operation{}
}
