package document_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestDuplicateServerName_BothServersLower is GitHub #631's repro compiled end
// to end: the repeat is reported, a warning at its own name naming the first,
// and nothing is dropped or merged, each server lowering with the name it
// declares. The load package's tests cover every list the rule reads.
func TestDuplicateServerName_BothServersLower(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://prod.example.com
    name: prod
  - url: https://prod-mirror.example.com
    name: prod
paths: {}
`)
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DuplicateServerName, ir.SeverityWarning),
		"one report: %+v", diags)
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DuplicateServerName, ir.SeverityWarning, "/servers/1/name"),
		"/servers/0/name")
	require.Len(t, doc.Servers, 2)
	for i, url := range []string{"https://prod.example.com", "https://prod-mirror.example.com"} {
		assert.Equal(t, "prod", doc.Servers[i].Name.Source)
		assert.Equal(t, url, doc.Servers[i].URLTemplate)
	}
}

// TestDuplicateServerName_CanonicalCollisionsAreNotReported is the tolerated
// half: servers whose lowered names collide although the document never wrote
// one name twice. A hint colliding with a hint, a declared name equal to
// another entry's hint, and two names equal only once canonicalized are all
// silent, since an emitter uniquifies them. Each row first proves its collision
// is real.
func TestDuplicateServerName_CanonicalCollisionsAreNotReported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec string
		// collide picks the two lowered words a row claims are equal, and the
		// row requires them to be, non-empty: it pins a boundary only if the
		// collision really happens rather than by passing vacuously.
		collide func(a, b ir.Naming) (string, string)
	}{
		{
			name: "two unnamed servers whose URL hints collide",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
servers:
  - url: https://api.example.com/v1
  - url: https://api.example.com/v-1
paths: {}
`,
			collide: func(a, b ir.Naming) (string, string) { return a.Hint, b.Hint },
		},
		{
			name: "a declared name equal to another entry's hint",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://api.example.com/v1
  - url: https://other.example.com
    name: https_api_example_com_v_1
paths: {}
`,
			collide: func(a, b ir.Naming) (string, string) { return a.Hint, b.Canonical },
		},
		{
			name: "two names that differ only in case",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://a.example.com
    name: prod
  - url: https://b.example.com
    name: Prod
paths: {}
`,
			collide: func(a, b ir.Naming) (string, string) { return a.Canonical, b.Canonical },
		},
		{
			name: "two names that differ only in their separator",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://a.example.com
    name: prod-eu
  - url: https://b.example.com
    name: prod_eu
paths: {}
`,
			collide: func(a, b ir.Naming) (string, string) { return a.Canonical, b.Canonical },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)

			assert.False(t, openapitest.HasDiag(diags, diag.DuplicateServerName),
				"nothing is reported: %+v", diags)
			require.Len(t, doc.Servers, 2)
			a, b := tc.collide(doc.Servers[0].Name, doc.Servers[1].Name)
			assert.NotEmpty(t, a, "the row compares lowered words, so they must be present")
			assert.Equal(t, a, b, "and they must really collide, or the row asserts nothing")
		})
	}
}
