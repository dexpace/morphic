# Values

`MaterializeValues` renders defaults, consts, enum values, literals, and examples as language
literals. `Num` (`BigVal` decimal string) parses straight to the target numeric/decimal type —
**never through float64** (§13 test T-5). `symbol` renders as a string only *explicitly* and
diagnosed (Erlang atoms: `ok ≠ <<"ok">>`); `Ref` renders as an `Enum.Member` reference, not an
inlined copy; `Ctor` renders as a constructor *call* (`plainDate.fromISO(...)`), never folded to a
frozen literal. A value of a type only a user's type-table row knows is decoded from its wire form
through the row's codec (§2.6).

## A default is documented, never sent

A property's or parameter's `Default` is what the API
assumes when the input is absent, so an SDK writes it into the documentation of the input and into
no request: an optional input the caller leaves unset is absent from the request whatever its
default, in every shape. Filling it in would turn "not set" into "set to the default", which on a
patch shape (`ModelShape.Patch`, §3.3) resets the field on every update, and on any other shape
overrides a server whose default has since moved. A required input with a default stays required.
A response member that arrives absent stays absent and is never read as its default, since presence
is a state the SDK keeps apart (INV8, §4.7).
