// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package testspec

import "slices"

// Tiny is a minimal but non-empty OpenAPI 3.1 document with a single /ping
// operation, titled "Tiny" so tests can assert the compiled document's Name.
const Tiny = `openapi: 3.1.0
info: {title: Tiny, version: "1"}
paths:
  /ping:
    get:
      operationId: ping
      responses: {"200": {description: ok}}
`

// Minimal is the smallest valid OpenAPI 3.0 document: a title, a version, and
// no paths.
const Minimal = `openapi: 3.0.0
info: {title: t, version: "1"}
paths: {}
`

// BadHeader is a syntactically valid YAML document that is not a usable
// OpenAPI spec: the response header's required is the string "notabool"
// rather than a bool, mirroring the committed
// testdata/openapi/resolve_target_invalid.yaml fixture. It compiles to a
// non-OK outcome, so a sweep flags it.
const BadHeader = `openapi: 3.1.0
info: {title: Broken, version: "1"}
paths: {}
components:
  responses:
    Bad:
      headers:
        X:
          schema: {type: string}
          required: notabool
`

// RefShapedSpec is one row of [RefShapedData]: a name and the spec it names.
type RefShapedSpec struct {
	Name string
	Data string
}

// RefShapedData returns specs where a structure shaped like a $ref sits outside
// a schema position, or aliases are used without a loop, so nothing in them is a
// cycle. The end-to-end test pins that, and the cycle detector's fuzz target
// seeds from the same rows. Each call returns a copy.
func RefShapedData() []RefShapedSpec { return slices.Clone(refShapedData) }

var refShapedData = []RefShapedSpec{
	{"example-data-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A:
      type: object
      example:
        p: {$ref: '#/components/schemas/A/example/q'}
        q: {$ref: '#/components/schemas/A/example/p'}
`},
	{"default-data-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A:
      type: object
      default:
        p: {$ref: '#/components/schemas/A/default/q'}
        q: {$ref: '#/components/schemas/A/default/p'}
`},
	{"extension-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-foo: {a: {$ref: '#/x-foo/b'}, b: {$ref: '#/x-foo/a'}}
`},
	{"responses-ref-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  responses:
    A: {$ref: '#/components/responses/B'}
    B: {$ref: '#/components/responses/A'}
`},
	{"allof-wrapped-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {allOf: [{$ref: '#/components/schemas/B'}]}
    B: {allOf: [{$ref: '#/components/schemas/A'}]}
`},
	// The four cases below are the negative controls for GitHub #26: each uses
	// the same alias/merge/contentSchema shapes as the crashing reproducers, but
	// legally (no degenerate cycle), so neither the alias scan nor the binder may
	// start reporting valid documents it previously accepted.
	{"legal-anchor-reuse", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {s: &s {type: object}}
components:
  schemas:
    A: *s
    B: {type: object, properties: {p: *s}}
`},
	{"legal-merge-key", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {base: &base {description: base schema}}
components:
  schemas:
    A: {<<: *base, type: object}
`},
	{"legal-content-schema", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {type: string, contentMediaType: application/json, contentSchema: {type: object}}
`},
	{"legal-alias-ref-terminates", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {ref: &r '#/components/schemas/B'}
components:
  schemas:
    A: {$ref: *r}
    B: {type: object}
`},
}
