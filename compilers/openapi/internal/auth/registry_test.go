// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestRegistrySchemes_SharesNothingWithTheRegistry pins the copy: what a caller
// does to the map it is handed reaches no later reading of the registry.
func TestRegistrySchemes_SharesNothingWithTheRegistry(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Components: &soa.Components{
		SecuritySchemes: sequencedmap.New(
			sequencedmap.NewElem("ghost", (*soa.ReferencedSecurityScheme)(nil))),
	}}
	reg, _ := auth.LowerSecuritySchemes(lowering.Ctx{Doc: doc})

	clear(reg.Schemes())

	assert.Contains(t, reg.Schemes(), ids.UnresolvedAuth("ghost"))
}
