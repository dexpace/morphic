# Naming in Go

## Reserved names

What each [scope](../../refine/reserved-names.md) reserves in Go:

- **The units**: `internal`, which no caller outside the SDK may import, `testdata`, which `./...`
  skips, and `vendor`, which the `go` command reads at a module's root as vendored dependencies;
  `main`, which makes a package a program, and `init`, which names only a function, so neither
  imports under its name (with Go 1.27.3, `import "example.com/sdk/main" is a program, not an
  importable package` and `cannot import package as init - init must be a func`); `documentation`, a
  package clause `go/build` ignores, so its directory builds nothing (`build constraints exclude all
  Go files`); a keyword, which no package clause takes (`expected 'IDENT', found 'type'`); each
  Windows device name ([emit rules](../../emit/rules.md)), which the `go` command refuses as an
  element of an import path on every host (`malformed import path "example.com/sdk/aux": "aux"
  disallowed as path element component on Windows`); and each predeclared identifier, which an
  import of a unit so named hides in the importing file: with Go 1.27.3, a caller importing units
  `error`, `string`, `len` and `nil` failed where its own code wrote them (`string (package name) is
  not a type`, `use of package len not in selector`). A caller can name such an import apart, but
  must in every file, and the identifiers are ones nearly every file writes. They are a table of the
  options version, read from the Go release's universe block, since a release can add one, as Go
  1.21 added `min`, `max` and `clear`, and a unit an SDK shipped must not move with it. No group of
  the golden spec is named for one (spec @bc6fad24, morphic @fa253fe19, each group's words joined).
  A unit so named takes the escape, `main_`, `documentation_`, `type_`, `aux_` or `error_`, which
  each built, a caller's file importing `error_` and `string_` among them.
- **A unit**: `Client`, its option type and their constructors, a group unit's sub-client, which no
  function of the unit builds ([the runtime](overview.md#what-a-caller-reaches-of-the-runtime)), and
  in the unit holding a service's receiver, `Receiver`, `NewReceiver`, `ReceiverOption`,
  `WithVerify`, `WithUnauthenticated` and the option named for each scheme ([operations the API
  sends](operations.md#operations-the-api-sends)). The runtime's names, `APIError`, `Body`,
  `EnvelopedResponse`, `OtherResponse`, `RawBody`, `Response` and `Stream` among them, are its own
  unit's, which holds no spec name ([the runtime's
  unit](../../refine/output-units.md#the-runtimes-unit)), and code elsewhere writes them qualified,
  `runtime.APIError`, so they meet no spec name.
- **A type**: `MarshalJSONTo`, `UnmarshalJSONFrom`, `Validate`, the redacting `Format`, `String`,
  `GoString` and `LogValue`, and the field holding the catch-all ([`User`](models.md#user-in-go)).
  The struct Go gives a closed union declares those codecs and redacting forms, `Validate` and
  `Value` ([unions](unions.md)), and a wrapper all of them but `Validate`; no spec name lands in
  either. An inclusive union's struct declares those codecs and forms, `Validate` and, when open,
  `Unknown` beside a field per variant ([unions](unions.md#an-inclusive-union)), and each field
  yields to them.

## File names

Where Go names a file for a declaration, as `user.go` for `User` ([`User`](models.md#user-in-go)),
the name is the declaration's words, joined with no separator, case-folded by the fold the engine
compares paths by ([the path fold](../../emit/rules.md)), and each character then outside `a`–`z`
and `0`–`9` written as `u` and its code point in lowercase hex, as the character-name table writes
one it does not hold ([below](#package-directories)): the neutral words its `Ident` starts as
([naming](../../refine/naming.md)), or for a name with none those its characters' names give, which
read nothing but the name and the character-name table
([naming](../../refine/naming.md#a-name-with-no-words-takes-its-characters-names)). Declarations of
one unit whose file names come out alike share a file, which holds them in ID order, so `Setup` and
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
`ServerLinux` in `server_linux.go` failed where it used them (`undefined: UserTest`). `go/build`
reads a platform only after an underscore before the name's first dot, so a name with none is read
against no list at all, and a Go release adding a port to its lists, which already name ports Go
does not build, such as `hurd` and `zos`, cannot change which files an SDK generated earlier builds.
`go list` listed the files of the six golden-spec types whose last word is a platform's or `test`,
`botmanagementenablejs.go` and `observatorypagetest.go` among them, and `v1386.go`, on each of the
47 ports `go tool dist list` gives, where a planted `x_js.go` was listed on `js/wasm` alone. T-19
holds it.

A file name that is a Windows device name before its first dot takes a trailing underscore,
`aux_.go` for a type `Aux`. `go build` builds a package holding `aux.go`, but its module is one no
caller can download ([emit rules](../../emit/rules.md)). The underscore leaves an empty word after
it, which `go/build` reads as no platform: `go list` listed `aux_.go` on all 47 ports. Characters
past ASCII are escaped rather than kept, since a module may hold a file name only of ASCII letters,
digits, some punctuation and Unicode letters: `golang.org/x/mod` 0.42.0 refused `cafe` and U+0301
COMBINING ACUTE ACCENT as a file name (`invalid char`) and `x٣.go`, a digit past ASCII, while once
escaped every name is ASCII, where every file system's fold agrees. A name whose part before `.go`
passes the cap a path segment holds, 110 bytes
([naming](../../refine/naming.md#a-name-too-long-for-a-path-is-shortened)), as an escaped name of 22
characters past ASCII can, keeps its first 102 bytes and the first eight hex digits of the SHA-256
of the whole, so the declarations sharing a file still share it.

The runtime's templates write their files in its own unit, which holds no declaration of the spec's
([the runtime's unit](../../refine/output-units.md#the-runtimes-unit)). The template writing each
operation's row of the runtime's settings renders beside the methods reading it instead ([runtime
policy](../../contract/runtime-policy.md#delivery)), so its file joins two words with an underscore,
which no declaration's file name holds between two words: `operation_settings.go`, which the `go`
command builds for every platform, its last word being neither `test` nor a `GOOS` or `GOARCH`
value, as `go list` showed on each of the 47 ports. Every file a template writes whole in a unit
holding spec declarations is named so, the client's, with its options and constructors, and the
receiver's among them: `api_client.go` and `api_receiver.go`, which `go list` listed on the 47 ports
beside a `client.go` declaring a spec type `Client_`. Named `client.go`, the template's file and
that type's would be two artifacts on one path, which the engine refuses ([emit
rules](../../emit/rules.md)). A declaration of the target's that refine builds as typed nodes is
named from its words like any other, and shares its file with a spec declaration of the same words.

## Package directories

A unit's package name is its directory's, the last element of its import path, so a caller writes
the name its import ends in. An import path takes ASCII letters, digits and `-._~+` alone: with Go
1.27.3, a root importing a unit `straße` failed (`malformed import path "example.com/sdk/straße":
invalid char 'ß'`). So a unit's name is rendered as a declaration's file is ([above](#file-names)):
its words joined, folded by the path fold, and each character outside `a`–`z` and `0`–`9` written as
`u` and its hex code point. A group `Straße` is the package `strasse`, `日本` the package
`u65e5u672c`, and both built where `straße` did not; on the golden spec, whose groups' names are
ASCII, the rule renders each as its words joined in lowercase. A package clause that kept the name
and a directory that escaped it, `package straße` in `strasse`, would have each caller write a name
its import does not show. The units' scope compares the names so rendered, so `Straße` and `Strasse`
collide there as `Webhooks` and `webhooks` do ([output units](../../refine/output-units.md)). A
package name is the one Go identifier written into a path, so it is the one Go role held to the
length cap ([naming](../../refine/naming.md#a-name-too-long-for-a-path-is-shortened)).

A name the options give, the root's module path or a partition's unit, never yields, so one that
`golang.org/x/mod/module.CheckPath` refuses, or whose last element is a units name above, is an
options error, a device name or a character past ASCII in any element among them, and so is a path
whose first element holds no dot, `corp/sdk` (`missing dot in first path element`), which names no
module the `go` command can fetch, though `CheckImportPath` takes it. T-19 and T-54 hold this
section.

## The escape

Go's casing lifts exported names clear of its keywords, so only a local, a parameter and a unit can
meet one: a keyword (`type`, `default`), a predeclared identifier a parameter would shadow
(`string`, `error`), a package its file imports (`time`, `url`), a name the method's body binds
(`ctx`, `params`) or a name of its unit the body writes, such as its operation's row of the
runtime's settings, takes a trailing underscore, and so does an exported name the target declares
itself (`Client`) and a unit named for a units name (`internal_`, `type_`). A parameter shadowing a
package-level name its body reads failed (`cannot use operationSettings["list"] (value of type byte)
as int value`).

## Imports

Go's exported names cannot meet its lowercase package names, and where two packages one Go file
imports share a name — the standard library's `time` and a group unit named `time`, the standard
library's `runtime` and the runtime's unit, or two rows' `decimal` — one takes a name of its own in
that file. A parameter or a local can hide one, `time.Second` failing beside a parameter `time`
(`time.Second undefined (type string has no field or method Second)`), so it takes the escape above.
An import in turn yields to a name the generated code binds, which never yields, and to a
predeclared identifier its file writes, which a package a type-table row imports can be named for
though no unit is: with Go 1.27.3, a unit `ctx` imported beside a parameter `ctx context.Context`
failed (`ctx.Thing is not a type`), as a package `error` did in a file returning `error` (`error
(package name) is not a type`), and each built imported as `ctx2` and `error2`. Of two imports
sharing a name, one the target's own code names, a standard library's or the runtime's, keeps it,
two such, as the standard library's `runtime` and the runtime's unit, being named apart by the
template writing them, and of two of the spec's units, the one whose group's ID sorts first keeps
it. Nothing else a Go file declares takes a name its references begin with: Go nests no type, a
struct field or a method is named only through its value (`u.Time`, `u.JSON()`), and an exported
name meets no package's and no predeclared identifier, all of which are lowercase. With Go 1.27.3, a
struct with fields `Time`, `JSON`, `time` and `json` and a method `Json` built beside imports of
`time` and `encoding/json`, and its methods reached both packages. T-55 holds it.
