# Target options

A target's options are a typed Go struct of its own, decoded by its `Configure` into the generator
that runs under them ([the contract](emitter.md#a-target-is-configured-once)). The engine
configures every target a run needs before any of them generates, so a mistake in a configuration
is reported before anything is generated rather than discovered in the output. `raw` is the
target's configuration as one JSON object, which the engine reads from the project's configuration
([the project](project.md)). No options value crosses the contract: a generator reads what its own
`Configure` decoded.

Every versioned configuration is decoded by one function, in `emitters/options`: a target's
options, the project's configuration and the plan's policy ([plan
policy](../plan/policy.md#the-policy-is-data)). A configuration differs from another in its type
and its versions, which are data, and never in how `Decode` reads a version, a default or a
`null`. A runtime-policy layer, a hint layer and a user's type table state no version of their
own, and `DecodeLayer` reads each by the same strict rules, over the defaults its caller hands it,
refusing every `null` because a layer leaves out any member it does not set: a runtime-policy
layer's are those of the version the project's configuration states, and a hint layer and a type
table have none ([delivery](runtime-policy.md#delivery), [shaping hints](shaping-hints.md), [type
table](type-table.md)).

```go
package options

// Schema is a configuration's versions. Versions[i] is version i+1, and it holds one at least.
type Schema[T any] struct{ Versions []Version[T] }

type Version[T any] struct {
    Defaults T                                               // this version's defaults, in the
                                                             // current shape
    Migrate  func(members map[string]jsontext.Value) error   // the shape change from the version
                                                             // before, on the raw members; nil = none
}

// Decode reads raw, a JSON object stating "version", and returns the value in the current shape,
// the version it stated, and the upgrade report under behindCode.
func Decode[T any](schema Schema[T], raw []byte, behindCode string) (value T, stated int, diagnostics []ir.Diagnostic, err error)

// DecodeLayer reads raw, a JSON object stating no version, over a copy of defaults, by Decode's
// rules but that it refuses every null.
func DecodeLayer[T any](defaults T, raw []byte) (T, error)

// Canonical returns value in the one form a configuration is digested in (below).
func Canonical[T any](value T) ([]byte, error)
```

- **Strict.** An unknown key, a repeated key, a missing required field and an invalid value are an
  error, as a compiler's unknown setting is (architecture.md §2.1): a setting silently ignored
  leaves its author believing they configured something. A fault is a Go error,
  the invocation's, and no diagnostic.
- **Versioned.** A configuration states the options `version` it was written against. One that
  states none is refused, and so is an absent configuration, because the version decides which
  defaults apply and that must not be guessed. A version newer than the target knows is refused: it
  was written for a later Morphic. Versions run from 1 with no gap.
- **`null` is a value only where a setting has none.** A member written `null`, at any depth, is
  refused unless its setting's type holds a value for none, a pointer in Go, as the plan's
  `FirstPage` does, where `null` turns the guess off ([plan
  policy](../plan/policy.md#a-page-walks-first-page)). Anywhere else, decoding one over a default
  would zero the default in silence (json/v2 in Go 1.27.3 reads `{"name": null}` over a default
  `name` as the empty string, and `{"rows": null}` as no table), and a configuration that means
  "the default" leaves the member out. The member's own type decides, read by the codec as it
  decodes, so no second reading of a configuration's names can disagree with the first. The
  pointer exception is `Decode`'s alone: a member a rule of its own decodes is refused what that
  rule refuses, and the type table's, a runtime-policy layer's and a hint layer's each refuse every
  `null`, a type-table row's `dependency` and a hint's `remount` among them
  ([type table](type-table.md), [delivery](runtime-policy.md#delivery), [shaping
  hints](shaping-hints.md)).
- **Defaults belong to a version.** A target declares each optional setting's default per version,
  and `Decode` fills what a configuration leaves unset with the defaults of the version it states:
  it decodes over a copy of that version's `Defaults`, a map merged key by key and a struct member
  by member. A change that would alter what existing configurations generate is a new version whose
  defaults differ, so an existing configuration keeps generating what it did until its owner moves
  it. New configurations start at the current version. `Defaults` is a value and not a function, so
  of a schema only `Migrate`, which is logic by nature, is code.
- **Migrations change the shape, never a default.** A setting renamed, retyped or removed between
  versions has one pure migration per version step, applied in order to the raw configuration, so
  `Generate` sees only the current shape. A migration writes no default: those come from the stated
  version, so no migration can carry the wrong ones forward.
- **The upgrade report.** Decoding a configuration behind the current version reports, as info
  diagnostics (`ir.SeverityInfo`, `ir.NoSource`, since decoding reads no IR document) under
  `behindCode`, which a target gives as `emitters/<target>/options-behind`, each setting it leaves
  unset whose default differs between its version and the current one, under the name the current
  shape gives it, so an owner sees exactly what moving changes. For a map setting merged key by key
  (such as a naming table), the report diffs entries key by key and reports each default entry
  added, changed or removed between those versions that the configuration does not set, as
  `typemap.Moved` does for a type table. The report is sorted by each setting's path in the current
  shape, a map's entries by key, so it reads alike on every run (INV7) though Go ranges over a map
  in no fixed order. The manifest ([write / integrate](../write-integrate.md)) records the stated
  version, `Configuration.Version`.
- **One canonical form.** `Configuration.Canonical` ([the
  contract](emitter.md#a-target-is-configured-once)) is the decoded value as `Canonical` writes it:
  RFC 8785's form with each integer kept as written, which is what
  `jsontext.Value.Canonicalize(jsontext.CanonicalizeRawInts(false))` leaves unchanged — members
  sorted by name, no whitespace, each string in its shortest escaping, valid UTF-8 and no name
  twice. RFC 8785 alone writes every number as a double, and `Canonicalize` wrote
  `9007199254740993` as `9007199254740992` (Go 1.27.3), so two configurations would digest alike.
  The IR's encoding, `json.Deterministic`, keeps a struct's members in the order Go declares them,
  which nothing reading the bytes alone can confirm. This form can be: `emitters.Configure` refuses
  bytes that call would change, and a target reordering its options struct moves no digest.

T-77 holds these rules.

## Three kinds of versioned data

What an options version fixes is of three kinds, and `Defaults` holds only the first:

- **Settings**, and tables whose entries stand alone, are in `Defaults`. A naming table is one: an
  acronym a configuration adds joins the built-in ones, and one it respells replaces that entry
  ([naming](../refine/naming.md)). Merging key by key is the right rule for them.
- **A table whose entries are replaced whole** is never in `Defaults`. Decoded over defaults, a
  user's type-table row stating a `Type` alone kept the built-in row's `Codec` and `Imports`
  (json/v2, Go 1.27.3), a type beside another type's import. So the configuration holds the user's
  entries alone, and the target merges them over the stated version's built-in table by that
  table's own rule, as `typemap.Merge` replaces a row whole ([type table](type-table.md)). The
  upgrade report for such a table is the table's too: `typemap.Moved` lists the built-in rows that
  differ between the stated version and the current one and that the user's table does not
  replace.
- **A table no configuration replaces** is not in the options type at all: the reserved names, the
  characters an identifier cannot hold, the case tables, a limit. A target reads each from its own
  table of the stated version.

A golden test per version pins all three: it decodes a configuration that states only that
version, and holds every default it fills and every fixed table the version reads, so a default or
a table changed in place fails a test rather than reaching existing SDKs.

## What a target's options hold

Per-target heuristics live here too, beside the target's other choices, rather than in the plan: the
parameter-passing shape ([request shaping](../refine/requests.md)), the type-table rows that give an
unbounded number a width ([type table](type-table.md)), and the naming tables
([naming](../refine/naming.md)) — the acronyms, the character names that give words to a name with
none, the leading words an identifier takes that would begin with what its role cannot, and the
length cap on a name a path holds. Which characters an identifier cannot hold is a table of the
options version as well, but one the target's toolchain fixes, so no configuration replaces it, and
so are the letter case and the character categories casing reads, a named Unicode version's and
never those of the Go release that builds Morphic. The path fold, which names are compared by too,
is `pathfold`'s and reads the building release's tables, which fold every assigned character alike
([emit rules](../emit/rules.md)). The target-AST nodes a default shapes carry `Inferred` provenance
([target AST](../refine/target-ast.md)), as INV6 requires. How a failure reaches its caller is a
setting here as well ([failures](../operations/errors.md#how-a-failure-reaches-its-caller)).
