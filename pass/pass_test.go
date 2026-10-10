package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

func TestPass_ValidateAdapterReturnsItsInputWithDiagnostics(t *testing.T) {
	t.Parallel()

	p := pass.NewValidate()
	doc := &ir.Document{Types: ir.TypeRegistry{}}
	out, diags := p.Run(doc)

	assert.Equal(t, "validate", p.Name())
	assert.Same(t, doc, out, "an analysis returns its input unchanged")
	assert.Equal(t, pass.Validate(doc), diags)
}
