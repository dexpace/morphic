// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nilPopulatePanic pins nil-populate-panic: building the model of a Reference
// Object whose $ref is null, or of an additionalOperations entry that is no
// mapping, panics on the caller's goroutine.
func nilPopulatePanic(t *testing.T) {
	tests := map[string]string{
		"a null $ref": head31 + "paths:\n  /a:\n    get:\n      parameters:\n        - $ref:\n" +
			"      responses: {'200': {description: ok}}\n",
		"an additionalOperations entry that is no mapping": "openapi: 3.2.0\ninfo: {title: t, version: '1'}\n" +
			"paths:\n  /a:\n    additionalOperations:\n      PURGE: ~\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := probe(t, "unmarshal", input)
			require.Equal(t, returned, r.outcome, revisit(t, r))
			assert.True(t, strings.HasPrefix(r.report, recoveredMark), revisit(t, r))
			assert.Contains(t, r.report, "nil pointer dereference", revisit(t, r))
		})
	}
}

// stringAsBoolean pins string-as-boolean: a boolean field decodes true from the
// string scalars "yes" and on, and a false that is no nil from any other string.
func stringAsBoolean(t *testing.T) {
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(head31+
		"paths:\n  /a:\n    get:\n      parameters:\n"+
		"        - {name: p, in: query, required: on, schema: {type: string}}\n"+
		"        - {name: q, in: query, required: \"yes\", schema: {type: string}}\n"+
		"        - {name: r, in: query, explode: \"true\", schema: {type: string}}\n"+
		"      responses: {'200': {description: ok}}\n"))
	require.NoError(t, err)
	pi, ok := doc.Paths.Get("/a")
	require.True(t, ok)
	params := pi.GetObject().Get().GetParameters()
	require.Len(t, params, 3)

	for i, name := range []string{"on", `"yes"`} {
		p := params[i].GetObject()
		require.NotNil(t, p.Required, name)
		assert.True(t, *p.Required, "%s: %s", name, revisit(t, result{}))
	}
	explode := params[2].GetObject().Explode
	require.NotNil(t, explode, revisit(t, result{report: `"true" decodes as nil`}))
	assert.False(t, *explode, revisit(t, result{report: `"true" decodes as true`}))
}

// head31 opens a 3.1 document with no paths written.
const head31 = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\n"
