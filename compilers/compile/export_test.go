package compile

import (
	"context"

	"github.com/dexpace/morphic/ir"
)

// Violations is the refusals Finish would hand over, read without finishing, so
// a test can observe each step that records one.
func (t *Types) Violations() []string { return t.refused }

// Settle is settle, for the tests that drive a settlement on its own.
func (t *Types) Settle(ctx context.Context) []ir.Diagnostic { return t.settle(ctx) }
