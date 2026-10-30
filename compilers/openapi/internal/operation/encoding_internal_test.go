// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

func TestEncodingConfig_NilEncoding(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	pe, unmodeled, diags := encodingConfig(l.ctx, l.types, nil, "/mp", "itemEncoding")
	assert.Equal(t, ir.PartEncoding{}, pe)
	assert.Nil(t, unmodeled)
	assert.Empty(t, diags)
}
