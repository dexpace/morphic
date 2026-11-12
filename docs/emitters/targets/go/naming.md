# Naming in Go

## Reserved names

What each [scope](../../refine/reserved-names.md) reserves in Go:

- **The units**: `internal`, which no caller outside the SDK may import, `testdata`, which `./...`
  skips, and `vendor`, which the `go` command reads at a module's root as vendored dependencies.
- **A unit**: `Client`, its option type and their constructors, and a group unit's sub-client and
  the function the root's client builds it with ([output units](overview.md#output-units)). The
  runtime's names, `APIError`, `Body`, `EnvelopedResponse`, `OtherResponse`, `RawBody`, `Response`
  and `Stream` among them, are its own unit's, which holds no spec name ([the runtime's
  unit](../../refine/output-units.md#the-runtimes-unit)), and code elsewhere writes them qualified,
  `runtime.APIError`, so they meet no spec name.
- **A type**: `MarshalJSONTo`, `UnmarshalJSONFrom`, `Validate`, the redacting `String`,
  `GoString` and `LogValue`, and the field holding the catch-all ([`User`](models.md#user-in-go)).
  The struct Go gives a closed union declares those codecs and redacting forms, `Validate` and
  `Value` ([unions](unions.md)), and a wrapper all of them but `Validate`; no spec name lands in
  either. An inclusive union's struct declares those codecs and forms, `Validate` and, when open,
  `Unknown` beside a field per variant ([unions](unions.md#an-inclusive-union)), and each field
  yields to them.

## File names

Where Go names a file for a declaration, as `user.go` for `User` ([`User`](models.md#user-in-go)),
the name is the declaration's words, joined with no separator and case-folded by the fold the engine
compares paths by ([emit rules](../../emit/rules.md)): the neutral words its `Ident` starts as
([naming](../../refine/naming.md)), or for a name with none those its characters' names give, which
read nothing but the name and the character-name table
([naming](../../refine/naming.md#a-name-with-no-words-takes-its-characters-names)). Declarations of
one unit whose words join alike share one file, which holds them in ID order, so `Setup` and
`SetUp`, whose words are `setup` and `set_up`, share `setup.go` and keep their names, since a Go
file holds any number of declarations and Go tells identifiers apart by case. So of Go's scopes only
the units' compares names case-folded, each unit being a directory
([collisions](../../refine/naming.md#collisions-and-the-names-a-generation-shipped)): renaming
`SetUp` would cost a Go caller the spec's name for nothing a Go build needs. Nor are two such files
named apart, `setup.go` and `setup2.go`, which would make a unit's files a scope of their own, each
name kept across generations as a symbol's is, where one shared file needs neither.

The name is read from the words, not from the identifier, since which declarations share a file must
be settled before `ComputeImports` gives each file its imports, and the naming steps settle an
identifier after it ([the refiner contract](../../refine/pipeline.md)). Files whose imports were
named apart can conflict once joined: with Go 1.27.3, a file declaring `Setup`, which imports the
standard library's `time`, and one declaring `SetUp`, which imports a unit named `time`, built,
while one file declaring both with those imports failed (`time redeclared in this block`). Nor is
the name the words in snake case, since the `go` command builds a file whose name ends in `_test.go`
only for the package's tests, and one ending in `_` and a `GOOS` or `GOARCH` value only for that
platform: with Go 1.27.3 on darwin/arm64, a package declaring `UserTest` in `user_test.go` and
`ServerLinux` in `server_linux.go` failed where it used them (`undefined: UserTest`). A file name
that is a Windows device name is #1492's. The runtime's templates write their files in its own unit,
which holds no declaration of the spec's ([the runtime's
unit](../../refine/output-units.md#the-runtimes-unit)). The template writing each operation's row of
the runtime's settings renders in the operation's unit instead ([runtime
policy](../../contract/runtime-policy.md#delivery)), so its file joins two words with an underscore,
`operation_settings.go`, which no declaration's file name holds, and which the `go` command builds
for every platform, its last word being neither `test` nor a `GOOS` or `GOARCH` value.

## The escape

Go's casing lifts exported names clear of its keywords, so only locals and parameters can meet one
there: a keyword (`type`, `default`), a predeclared identifier a parameter would shadow (`string`,
`error`), a package its file imports (`time`, `url`) or a name the method's body binds (`ctx`,
`params`) takes a trailing underscore, and so does an exported name the target declares itself
(`Client`).

## Imports

Go's exported names cannot meet its lowercase package names, and where two packages one Go file
imports share a name — the standard library's `time` and a group unit named `time`, the standard
library's `runtime` and the runtime's unit, or two rows' `decimal` — one takes a name of its own
in that file.
