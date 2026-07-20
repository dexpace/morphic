# morphic
Idiomatic SDKs and docs from any API spec — OpenAPI, Smithy, TypeSpec, GraphQL, and more. One IR, many targets.

## Design docs

- [Architecture](docs/architecture.md) — pipeline (compilers → IR → passes → emitters), package layout, contracts.
- [IR design](docs/ir-design.md) — the intermediate representation model: node catalog, semantics, per-format lowering.
- [Spec capability matrix](docs/ir-spec-matrix.md) — what each source format can express; the union the IR is designed against.
- [Prior art](docs/prior-art.md) — lessons taken from oagen, Kiota, and TypeSpec/TCGC.
