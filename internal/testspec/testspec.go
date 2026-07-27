package testspec

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
