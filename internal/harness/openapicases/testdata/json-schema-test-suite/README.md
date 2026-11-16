# Vendored JSON Schema Test Suite

A copy of part of the
[JSON Schema Test Suite](https://github.com/json-schema-org/JSON-Schema-Test-Suite), kept with its
`LICENSE` (MIT). `SuiteGroups` in `suite.go` reads it, embedded, as the groups that apply at JSON
Schema 2020-12. `Groups` writes each valid instance of each such group as the default of a use of
the group's schema, which the instance oracle holds to the type the use compiles to. The binding
oracle in `compilers/openapi/internal/canary/oracle_test.go` compiles each such group's schema that
writes a `$ref`, and compares what the compiler binds it to with what the parser library resolves
it to.

## Revision

The files are byte-identical to upstream `main` at `38dea932b02986f496345bf007108277777630bf`
(committed 2026-11-11). That is checked by hashing each vendored file with `git hash-object` and
comparing it with the blob SHA upstream's tree at that commit lists for the same path.

## Selection

The files are `validation/tests/<name>.json` for each top-level test file holding a group that
applies at 2020-12, kept at upstream's paths. The files holding none, the legacy and draft-3 ones,
are left out, as are `optional/` and `proposals/`. A group's `tags` are not read: they say whether
it expects `format` to assert, and a valid instance of a group expecting an assertion is valid
where `format` only annotates.

A group applies at a release by its `compatibility` expression, which `compatible` reads as
upstream's `validation/README.md` defines it: terms ordered smallest to largest and joined by
commas, `N` opening a range from release `N`, `<=N` closing the open range, or one from the first
release, at `N`, and `=N` naming release `N` alone. A group applies where any range holds the
release, and one writing no expression at every release. Releases are numbered as the suite numbers
them: a draft by its number, 2019-09 as `2019` and 2020-12 as `2020`.

A group is named `<file>/<description>`, with `#<n>` after the nth occurrence of a description its
file repeats, so a refresh renames a group only where upstream renames its description.

## Refreshing

1. Copy `LICENSE` and the selected files from upstream's new commit over these, keeping the paths,
   and update the revision above.
2. Run `go test ./internal/harness/openapicases -run TestSuiteGroups` to read them.
3. Run both oracles with `-update`:

   ```bash
   go test ./compilers/openapi/internal/canary -run TestBindingOracle_AgreesWithTheLibrary -update
   go test ./internal/harness/openapicases -run TestCore_ValidInstancesAreAdmitted -update
   ```

   They rewrite `compilers/openapi/internal/canary/testdata/binding_disagreements.txt` and
   `internal/harness/openapicases/testdata/instances.txt`, keeping the reason of each line still
   found and marking a new one `unclassified`.
4. Give each `unclassified` line a reason its test's reason map names, and run both tests again
   without `-update`.
