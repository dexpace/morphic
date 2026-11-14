# Vendored JSONPath compliance test suite

A copy of the [JSONPath Compliance Test Suite](https://github.com/jsonpath-standard/jsonpath-compliance-test-suite),
the cases for RFC 9535, kept with its `LICENSE` (BSD-2). `TestCompliance` in the package runs every
case of `cts.json` through the evaluator.

## Revision

`cts.json` and `LICENSE` are byte-identical to upstream at
`20ebdcce5008c8a777ee9c80ad853ecf606950b5`. The file holds 715 cases, 249 of them invalid selectors.

## Refreshing

Copy `cts.json` and `LICENSE` from the new upstream commit over these and update the revision above.
The test fails on a case that is neither passing nor in its skip table, and on a skip that passes.
