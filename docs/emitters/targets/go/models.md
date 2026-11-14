# Models in Go

## Optionality

The reference Go lowering is ogen's generic-box scheme (`gen/ir/generics.go`), which independently
arrives at exactly these four states: required-nonnull → bare `T`; optional → `Opt[T]` (a
`{value T; set bool}` box, so presence is distinct from the zero value); nullable → `Nil[T]`; both →
`OptNil[T]`. A mature real-world generator needing all four first-class boxes is the strongest
external confirmation of INV8, and — decisively — ogen makes this decision in its *emitter*
(`boxType`), keeping `Required` and `Nullable` orthogonal in the schema model right up to that
point. That is Morphic's split precisely: orthogonal bits in the IR, boxed in refine. Two
Go-specific optimizations to inherit: reference types (slice/map/pointer) can encode one of the four
states in their own `nil` (ogen's `NilSemantic: invalid | optional | null`), saving a wrapper;
`explicit` protobuf presence → pointer/hazzer, `implicit` → `omitzero,omitempty`, so a zero is not
serialized.

Presence is stated in `encoding/json/v2` terms, and `omitempty` never keeps a presence state. v2
defines `omitempty` by the encoded JSON — a field is dropped when it would encode as `null`, `""`,
`{}` or `[]` — so it keeps a zero number or `false` and drops a pointer to an empty struct: a set
but empty value turns into an absent one, the silent conflation [the shared
rule](../../refine/models.md#optionality--nullability--presence-inv8) forbids. `omitzero` is defined
by the Go value and asks the type's `IsZero` method when it has one. The boxes implement `IsZero` as
"not set" beside their own `MarshalJSONTo`/`UnmarshalJSONFrom`, so a box field tagged `omitzero` is
omitted exactly when unset, and an `Opt[int]` set to `0` is written. The `implicit` tag carries both
options because each alone misses a zero: `omitempty` keeps `0` and `false`, and `omitzero` keeps an
empty non-nil slice or map.

## `User` in Go

The IR and the plan are in [TypePlan](../../plan/types.md#a-model-with-lifecycle-visibility).

- **refine.** `ProjectVisibilityShapes` emits `StructType{Name:"User"}` (read) and
  `StructType{Name:"UserCreate"}` (create). `LowerOptionality`: `name`, required and non-null, is a
  bare `string`, and the optional `id`, `email` and `password` are `Opt[T]` boxes
  ([optionality](#optionality)), so an absent email stays apart from an empty one. `RenderCasing`
  turns `["email"]`→`Email`, and the tag is `Tag:json:"email,omitzero"`: the name from `WireName`,
  never the identifier, and `omitzero` because an unset box written without it would send
  `"email":null`. `password` is `Secret`, so it is a `runtime.Secret[Opt[string]]` ([a redacted
  member](#a-redacted-member)), `User`'s `String()`, `GoString()` and `LogValue()` show it as
  `[REDACTED]`, as they would a member whose type is `Sensitive`
  ([redaction](../../refine/redaction.md)), and it is excluded from the read shape. `LogValue` is
  what `log/slog` reads: its JSON handler writes any other value as JSON, the wire form, and
  resolves a `slog.LogValuer` only on the value it is handed, never on a list's elements, so
  `LogValue` writes each redacted position as the marker itself. All three take a value receiver: a
  pointer receiver's methods are not in a `User`'s method set, so `fmt` and `log/slog` would print
  field by field a `User` held by value, as `ListUsers` yields one and a slice holds one. `User`
  declares `Format(fmt.State, rune)` beside them, writing what `String` does, since `fmt` asks
  `String` and `GoString` under some verbs alone. `Additional:"closed"` → the model's
  `UnmarshalJSONFrom` decodes its members under `json.RejectUnknownMembers(true)`. Every model pins
  that option for itself ([the shared rule](../../refine/models.md#unknown-members)), `false` unless
  closed: a v2 option reaches every value below the call that sets it, so a closed model would
  otherwise make an open model nested in it strict, and a strict caller would reach a model the spec
  leaves open. A catch-all (`AdditionalProps`) is the struct's one embedded fallback, a
  `map[string]T` tagged `json:",embed"`, which receives the undeclared members whatever the options
  say. One whose `Key` is set is a `map[K]T`, `K` the key's Go type, tagged `json:"-"`, since
  json/v2 embeds only a map with string keys (`embedded Go struct field Extra of type
  map[int64]string must be a Go struct, Go map of string key, or jsontext.Value`): the model's codec
  pair receives the undeclared members in an embedded `map[string]T` of its own and reads each name
  as [a map's key](../../contract/type-table.md#a-maps-key-is-text) is read, refusing two names that
  read as one key, and writes each key's text back. With an `int64` key, `{"1": …, "2.0": …}` read
  as the keys 1 and 2, `{"1": …, "1e0": …}` and `{"q": …}` were refused, and a caller's own
  `json.Unmarshal` of a list of such models read them too (Go 1.27.3). Holding the names as sent, in
  a `map[string]T`, was the alternative, and keeps `"1"` and `"1.0"` apart where Kotlin reads one
  key. T-61 holds it.
- **emit.** Printer folds the two structs, prints through `go/format`. File `user.go` ([file
  names](naming.md#file-names)) in the root unit ([output units](../../refine/output-units.md)).
  Manifest records `User.ID → "User"` and `User.ID#create → "UserCreate"`, the shape's key ([the
  declaration a shape takes](../../plan/types.md#the-declaration-a-shape-takes)).

## A redacted member

A model's `Format` answers each verb `fmt` asks a method under, and `fmt` asks none under a verb it
reports as bad, `%w` outside `Errorf` or of a value that is no error and `%p` of a struct, nor of a
value below an unexported field: it prints the fields by reflection. So a plain field leaks there:
with Go 1.27.3, `log.Printf("call failed: %w", err)` of a failure holding a `User` printed the
password, and of 1,040 renderings of a model, every verb and flag through `Sprintf`, `Errorf`,
`log.Printf` and `log/slog`'s text handler, of one held directly, by pointer, in an exported and an
unexported field of a caller's struct, a slice and a map, 368 held the secret. No declaration on a
plain struct answers a rendering that asks it nothing. So each redacted position, a member, a
sensitive list element or map value, a variant wrapper's `Value`, a request type's or a headers
type's field, holds its value as a `runtime.Secret[T]`, `T` being what it lowers to otherwise, and a
map with sensitive keys is held whole:

```go
type Secret[T any] state.Secret[T] // the runtime's: a function returning the value, as Raw is

func SecretOf[T any](v T) Secret[T] // what a caller builds one from
func (s Secret[T]) Value() T         // the value; T's zero for a zero Secret
```

`Secret` declares the redacting forms, the codec pair, which writes and reads what `T` does,
`Equal`, and `IsZero`, which answers what `omitzero` does for a field of type `T` holding the value:
where `T` or its pointer declares `IsZero` it asks that, a nil pointer, a nil interface and an
interface holding a nil pointer being zero unasked, and otherwise whether the value is `T`'s zero,
which a zero `Secret` holds. So a redacted field is left out exactly where a plain field of its type
and tag is: of 41 values, an unset and a set box, a zero and a set number, a nil and an empty map
and slice, and a nil pointer to a type declaring `IsZero` on its value and on its pointer among
them, under `omitzero`, under `omitzero,omitempty` and under neither, the two wrote the same bytes
each time. Asking `T`'s `IsZero` of a nil pointer or interface panicked where the plain field wrote
`{}`, and an `IsZero` answering only for a `T` that declares one differed in 21 of the 123 writes,
`SecretOf(int64(0))` writing `"pin":0` under the implicit tag and a nil map `"tags":{}` (Go 1.27.3).
A function is the one kind `fmt` prints as an address however it is asked. A pointer was built
first: one to a list, a struct or a map is followed once under a verb `fmt` has no pointer form for,
`%s` among them, and printed a `*[]string`'s secret in 216 of the same renderings, where `Secret`
held it in none, nor did a failure holding the model. The costs are a function's: a caller wraps the
value it would otherwise write, `runtime.SecretOf(v)` with `v` an `Opt[string]` for `password`, and
reads `.Value()`; two models holding equal secrets are unequal to `reflect.DeepEqual`, and `==` does
not compile on one, so a test compares through `Equal`, which go-cmp asks; and a member a document
starts or stops marking changes type, a break for its callers, as a reference joining a cycle does
([recursive references](#recursive-references)). A value a caller takes out with `Value` is its own
and renders as Go renders it. T-43 holds this section.

## A shape that is an alias

A shape whose projection coincides with the read one
([TypePlan](../../plan/types.md#the-declaration-a-shape-takes)) is `type UserCreate = User`, in the
file of `User`, and each signature names the alias: `go doc` shows `func Create(b shared.UserCreate)
string`. A caller in another module that built `shared.UserCreate{Name: "Ada"}` compiled before and
after `UserCreate` became a struct of its own, where one that built a `shared.User` for the same
call stopped with `cannot use … as shared.UserCreate value` (Go 1.27.3).

A union's alias, and a polymorphic base's, which Go declares as it does a union
([unions](unions.md)), brings every name the type declares in its unit: `type PaymentCreate =
Payment`, `type PaymentCreateVariant = PaymentVariant`, an alias for each wrapper, the fallback's
among them, `type PaymentCreateCard = PaymentCard`, and a function for each factory, `func
NewPaymentCreateCard(v CardCreate) PaymentCreate`, which calls the type's own. A caller that built
`sdk.PaymentCreate{Value: sdk.PaymentCreateCard{Value: sdk.CardCreate{…}}}`, called a factory and
switched over a `sdk.PaymentCreateVariant`, one variant's type in another unit, compiled before and
after the three became declarations of their own. With the struct aliased alone, the same caller had
only `sdk.PaymentCard` to write and stopped at the split (`PaymentCard does not implement
PaymentCreateVariant`), and a caller building a `PetCreate` from `sdk.PetCreateDog` and
`sdk.NewPetCreateDog` compiled across the split of a base `Pet` and its subtype `Dog`. An inclusive
union's alias is its struct's alone, its variants being fields. T-60 holds this section.

## Nesting

json/v2 refuses a value nested past 10,000 levels, decoding and encoding (`jsontext: exceeded max
depth`), and no option moves that bound, which is where the range of [the
limit](../../refine/models.md#how-deep-a-value-nests) ends. Below it the runtime counts for itself:
before it decodes a text it scans the bytes once, counting the objects and arrays open outside a
string and the values, and refuses one nested past `MaxDepth` or holding more than `MaxValues` as a
body that does not decode. It encodes through a writer that scans the same way as the bytes arrive,
so an encode ends at the first byte past either limit, with an error and nothing sent: a value
sharing each child at 60 levels, 2^60 values as JSON, was refused in 0.4 s at the default. The depth
scan took 1.6 ms on a 1.1 MB body that json/v2 decoded in 6.7 ms, and a value at json/v2's bound
decoded in 3 ms on a goroutine's stack, which grows as it needs (Go 1.27.3, darwin/arm64). A count
kept in each model's `UnmarshalJSONFrom` would miss an `any` position, which json/v2 decodes with no
code of the SDK's.

A caller can build a value that holds itself, through a pointer, a slice or a map. Encoding refuses
it (`encountered a cycle`). Printing must not follow it: Go ends the process on a stack overflow
(`fatal error: stack overflow`), which no `recover` stops, and a `String` calling each nested
value's `String` did so on a model whose pointer led back to it. So each model, union struct,
wrapper and positional struct declares `Format(fmt.State, rune)`, which `fmt` asks before `String`
and for every verb it asks a method under ([a redacted member](#a-redacted-member)), and renders its
members itself, calling a nested value's `Format` with a state that carries the level and a count of
the values the whole print has written; at `MaxDepth` levels, and once that count passes
`MaxValues`, it writes `...` and returns. `String` and `GoString` print through it and `LogValue`
returns its text, so all three end, and a caller sees `...` where the value went on. Measured so, a
value holding itself through a pointer, a slice and a map each printed 64 levels and stopped, as did
one held from another unit, and `%d`, `%x` and `%q` showed a redacted member as `%v` does. The level
alone does not end a print: a value holding itself twice would write 2^64 values within it, and with
the count unread 26 levels took 67 million calls and 7.7 s, where the count stopped it after
16,777,217 calls, 168 MB and 1.4 s. Where `fmt` asks no method, it follows a slice or a map into
itself until the process ends (`goroutine stack exceeds 1000000000-byte limit` for `%w` of a value
whose list held it), which no declaration answers; a decoded value is a tree, so only a value its
caller built to hold itself meets it. The state is a type of the module's own under `internal/`,
which no caller can name, and `Format` is a name no member takes ([reserved
names](naming.md#reserved-names)), as `String` is: 20 properties of the golden spec are named
`format` (spec @bc6fad24). Go generates no hash and no comparison of a model: `==` compares a
pointer by identity and is not defined over a slice or a map.

So no model is comparable, from its first generation: each model's struct opens with the field
`_ [0]func()`, which takes no space and which `==` and a map's key refuse. A struct that was
comparable stops being so when it gains a list, a map or a box over one, the commonest change an
API makes, and a caller's `==` then fails to build, a break for a member added. Measured with Go
1.27.3 and x/exp `a52e3aef5920`: a struct of one string gaining a `[]string` member was reported
incompatible by `apidiff` (`old is comparable, new is not`), and compatible with the field in
place; the field left the struct's size as it was and `reflect.DeepEqual` as it answers; and it
refuses a literal written without field names, whose callers a member added would break as well.

`Validate` walks the same way: it hands its own `Format` that state marked as a check, and each
`Format` so asked runs its value's checks in place of writing it and passes the state to each nested
value's `Format`, so a value past `MaxDepth` levels or `MaxValues` values is reported as nested past
the limit and followed no further. The `error` `Validate` returns then answers `errors.Is(err,
runtime.ErrNesting)`, a sentinel of the runtime's unit as a kind's is
([failures](errors.md#ratelimited-in-go)), which a constraint's failure does not answer: a value
holding itself answered it, with a constraint failing beside it too, and a constraint's failure
alone did not, its type in another package. A `Validate` calling each nested value's `Validate`
ended the process on a model whose pointer led back to it (`fatal error: stack overflow`), and no
other exported method carries the state into another unit, since an exported signature naming an
internal type is refused (T-64). Measured so, with the checked type in another package, a value
holding itself was reported at the limit, and one sharing each child at 60 levels after 16,777,216
values in 1.5 s (Go 1.27.3). An inclusive union's `Format` passes the state to each set field's and
drops the constraint failures they reported once one passed, never a nesting failure: a union
holding itself through one field beside one that passes answered `ErrNesting` in either order of the
fields, as did one whose field held a value past either limit of a type in another package, where
dropping all they reported returned nil. The state keeps the first 1,000 constraint failures,
counting the rest, which the error's text gives, and the nesting failure beside them: the value
sharing each child at 60 levels, its every node failing two constraints, gave 1,000 and a 21 KB text
in 50 ms and 6 MB, where keeping all gave 16,777,216, a 352 MB text and 4.5 GB (Go 1.27.3). T-63
holds these.

## Recursive references

Go holds a struct field by value, so a cycle of struct fields is an invalid recursive type. A slice,
a map, a pointer and an interface each break a cycle, so a list, a map, a union and a polymorphic
base — the last two a struct holding a sealed interface, or a pointer per variant where the union is
inclusive ([unions](unions.md)) — already do; a model, a tuple's positional struct and the boxes
above hold their contents by value, and do not. So a reference is held through a pointer when it
lies on a cycle of references Go holds by value, read after `Base` and `Mixins` are flattened into
the declarations they land in. The [shared refine
helper](../../refine/models.md#recursive-references) finds those cycles. The pointer stands for a
box's state where it can: a required reference is `*T`, which `Validate` reports when nil; an
optional or a nullable one is `*T`, nil meaning absent or null; and an optional nullable one is
`Opt[*T]`.

Go breaks every reference on such a cycle rather than the fewest that would do. The fewest depend
on the whole graph — one type added elsewhere can change which reference is picked — and each pick
that moves changes a field's type for every caller. Nor does it break every by-value reference whose
two ends merely share a component: a cycle that passes through a list, a map or a union already
compiles, and a pointer on it would ask every caller to check for nil where nothing requires it.
A reference is a pointer exactly when a by-value cycle passes through it, which only a change to
that cycle moves. T-20 holds the rule.
