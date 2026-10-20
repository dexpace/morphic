# Vendored JSON Schema Test Suite

A copy of part of the [JSON Schema Test Suite](https://github.com/json-schema-org/JSON-Schema-Test-Suite),
kept with its `LICENSE` (MIT). The binding oracle in `oracle_test.go` compiles each schema of these
files and compares what the compiler binds a `$ref` to with what the parser library resolves it to.

## Revision

The files are byte-identical to upstream `main` at
`55719286c33ae0d1823bfb65fea10338a19bc37c` (committed 2026-10-14). That is checked by hashing each
vendored file with `git hash-object` and comparing it with the blob SHA the GitHub contents API
reports for the same path at that commit; all files, `LICENSE` included, match.

## Selection

The files are the suite's `tests/draft2020-12` files in which a group writes a `$ref`: `anchor`,
`defs`, `dynamicRef`, `infinite-loop-detection`, `items`, `ref`, `refRemote`, `unevaluatedItems` and
`unevaluatedProperties`, plus `optional/anchor`, `optional/cross-draft`, `optional/dynamicRef`,
`optional/ecmascript-regex`, `optional/id`, `optional/refOfUnknownKeyword` and
`optional/unknownKeyword`. The oracle takes from each file only the groups whose schema contains
`"$ref"`; `optional/ecmascript-regex` has no such group and yields no document.

## Refreshing

1. Copy the files from `tests/draft2020-12` at the new upstream commit over these, keeping the paths,
   and replace `LICENSE` if it changed. Update the revision above.
2. Run `go test ./compilers/openapi/internal/canary -run TestBindingOracle_AgreesWithTheLibrary -update`.
   It rewrites `../binding_disagreements.txt`, keeping the reason of each disagreement that is still
   found and marking a new one `unclassified`.
3. Write a reason for each `unclassified` line, one of the keys of `oracleReasons`, then run the
   test again without `-update`.

A disagreement is keyed by its document, `suite/<file>/<index of the group in the file>`. A refresh
that adds, removes or reorders groups shifts those indexes, so the keys of a file that changed in
step 1 move and their reasons have to be matched up again by reading the schema each now names.
