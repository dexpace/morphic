# Vendored Overlay Specification test sets

A copy of part of the [OpenAPI Overlay Specification](https://github.com/OAI/Overlay-Specification)
repository, kept with its `LICENSE` (Apache-2.0). `overlay_suite_test.go` in the parent directory
reads it.

## Revision

The files are byte-identical to upstream at `be39b56020da12b8db56080da90fecb470701831`.

## Selection

- `compliant-sets/`: the eight sets, each an `openapi.yaml`, an `overlay.yaml` and the `output.yaml`
  that applying one to the other gives, and the directory's own `README.md`.
- `tests/v1.0`, `tests/v1.1` and `tests/v1.2-dev`: the documents the upstream schema tests hold to
  each version, in `pass` and `fail` directories. The upstream test runner is not copied.

## Refreshing

Copy the same paths from the new upstream commit over these and update the revision above. The tests
assert the count of files they read, so an added or removed file fails them until the count follows.
