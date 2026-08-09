package annotation

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

func TestConstraints_NilSchema(t *testing.T) {
	t.Parallel()
	c, kept, diags := Constraints(nil, false, "/p", 0)
	assert.Nil(t, c)
	assert.Nil(t, kept)
	assert.Nil(t, diags)
}
func TestApplyExclusive_NumericWithoutRootNode(t *testing.T) {
	t.Parallel()
	f := 5.0
	s := &oas3.Schema{ExclusiveMinimum: &values.EitherValue[bool, bool, float64, float64]{Right: &f}}
	c := &ir.Constraints{}
	site := boundSite{pointer: "/p"}
	diags := applyExclusive(c, s, &site, true, false)
	// The numeric arm is taken (2020-12 dialect, numeric value) but there is no raw
	// node to read the exact literal from, so nothing is set and no diagnostic.
	assert.Nil(t, diags)
	assert.False(t, c.ExclusiveMin)
	assert.Empty(t, site.kept)
}
