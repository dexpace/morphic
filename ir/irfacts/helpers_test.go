// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package irfacts_test holds the tests of the irfacts package. The helpers
// several files share live here; the type builders they share with package ir's
// tests are irtest's.
package irfacts_test

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// typeIDReflect is the reflect.Type of ir.TypeID.
var typeIDReflect = reflect.TypeFor[ir.TypeID]()

// irPackage is the ir package type-checked, loaded once for the tests here that
// read its declarations.
var irPackage = sync.OnceValues(func() (*decls.Package, error) {
	return decls.Load("github.com/dexpace/morphic/ir")
})

// declaredKinds returns every TypeKind the ir package declares, sorted: read
// from the source, so a kind added there reaches every test ranging over it.
func declaredKinds(t *testing.T) []ir.TypeKind {
	t.Helper()
	p, err := irPackage()
	require.NoError(t, err)
	consts, err := p.StringConsts("TypeKind")
	require.NoError(t, err)
	kinds := make([]ir.TypeKind, 0, len(consts))
	for _, c := range consts {
		kinds = append(kinds, ir.TypeKind(c.Value))
	}
	slices.SortFunc(kinds, func(a, b ir.TypeKind) int { return strings.Compare(string(a), string(b)) })
	require.NotEmpty(t, kinds, "the ir package must declare its type kinds")
	return kinds
}
