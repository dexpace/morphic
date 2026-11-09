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
	"github.com/dexpace/morphic/ir"
)

// TestRegistrySchemes_SharesNothingWithTheRegistry pins the copy: what a caller
// does to the map it is handed reaches no later reading of the registry.
func TestRegistrySchemes_SharesNothingWithTheRegistry(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Components: &soa.Components{
		SecuritySchemes: sequencedmap.New(
			sequencedmap.NewElem("ghost", (*soa.ReferencedSecurityScheme)(nil))),
	}}
	c := lowering.Ctx{Doc: doc}
	declared, _ := auth.LowerSecuritySchemes(c)
	reg := auth.RegistryOf(c, declared)

	clear(reg.Schemes())

	assert.Contains(t, reg.Schemes(), ids.UnresolvedAuth("ghost"))
}

// TestRegistryOf_SharesNothingWithTheDeclaredSchemes pins RegistryOf's copy:
// what a caller does to the map it handed over reaches no reading of the
// registry, as what a requirement adds to the registry never reaches the map
// (operation.TestLowerService_LeavesTheDeclaredSchemesAsTheyWere).
func TestRegistryOf_SharesNothingWithTheDeclaredSchemes(t *testing.T) {
	t.Parallel()
	id := ids.Auth("key")
	declared := map[ir.AuthID]ir.AuthScheme{id: {ID: id}}
	reg := auth.RegistryOf(lowering.Ctx{Doc: &soa.OpenAPI{}}, declared)

	clear(declared)

	assert.Contains(t, reg.Schemes(), id)
}
