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

// Fulfil is fulfil, for the tests that drive the promised builds on their own.
func (t *Types) Fulfil(ctx context.Context) []ir.Diagnostic { return t.fulfil(ctx) }

// Len is how many types t interns, for the tests that count what a step
// interns without finishing.
func (t *Types) Len() int { return len(t.reg) }
