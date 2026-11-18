// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// everywhere returns the rows table reading every position with rules.
func everywhere(rules oas.SchemaRules) dialect.Table {
	return dialect.NewBuilder(rules).Table()
}

// rulesAt returns the Schema Object's rows at version in the dialect named.
func rulesAt(t *testing.T, version, named string) oas.SchemaRules {
	t.Helper()
	r, unread := oas.Table().SchemaRules(version, named)
	require.False(t, unread, "%s %s", version, named)
	return r
}

// document returns an OpenAPI document of version whose components are
// schemas, each a line written in flow style.
func document(version string, schemas ...string) string {
	return openapitest.ComponentSpecVer(version, "    "+strings.Join(schemas, "\n    ")+"\n")
}

// tableOver loads spec and returns a table reading its bindings with rules,
// and the context positions in its document are taken from.
func tableOver(t *testing.T, spec string, rules oas.SchemaRules) (*statement.Table, lowering.Ctx) {
	t.Helper()
	c := servicetest.Context(t, spec, service.Policy{})
	return statement.NewTable(statement.Source{Bindings: c.Bindings(), Rules: everywhere(rules)}), c.Ctx
}

// component returns the position of the component schema name.
func component(c lowering.Ctx, name string) bind.Position {
	return c.Position(jsontext.Pointer("/components/schemas/" + name))
}

// keyNames lists the names of the keys s writes, in order.
func keyNames(s *statement.Schema) []string {
	out := make([]string, 0, len(s.Keys))
	for _, k := range s.Keys {
		out = append(out, k.Name)
	}
	return out
}
