# Naming in Go

## Reserved names

What each [scope](../../refine/reserved-names.md) reserves in Go:

- **The units**: `internal`, which no caller outside the SDK may import, `testdata`, which `./...`
  skips, and `vendor`, which the `go` command reads at a module's root as vendored dependencies.
- **A unit**: `Client`, its option type and their constructors, a group unit's sub-client, and any
  runtime name it declares there (`APIError`, `Body`, `OtherResponse`, `Response`, `Stream`).
- **A type**: `MarshalJSONTo`, `UnmarshalJSONFrom`, `Validate` and a redacting `String`, and the
  field holding the catch-all ([`User`](models.md#user-in-go)). The struct Go gives a union declares
  those codecs, `Validate` and `Value` ([unions](unions.md)), and a wrapper those codecs and
  `Value`. No spec name lands in either, so none yields there.

## The escape

Go's casing lifts exported names clear of its keywords, so only locals and parameters can meet one
there: a keyword (`type`, `default`), a predeclared identifier a parameter would shadow (`string`,
`error`), a package its file imports (`time`, `url`) or a name the method's body binds (`ctx`,
`params`) takes a trailing underscore, and so does an exported name the target declares itself
(`Client`).

## Imports

Go's exported names cannot meet its lowercase package names, and where two packages one Go file
imports share a name — the standard library's `time` and a group unit named `time`, or two rows'
`decimal` — one takes a name of its own in that file.
