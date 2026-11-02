# Structure and boilerplate

## The structure-vs-boilerplate line (the honest boundary)

| Content | Representation | Why |
|---|---|---|
| Models, enums, unions, discriminators, operations, clients, serializers | **Typed AST nodes** | Spec-derived *structure*; correctness must be compiler-checked; must be uniform across languages |
| Runtime core: HTTP transport, retry loop, backoff+jitter, pagination iterator runtime, a stream reader per framing, the error and response types, auth injectors, logging hooks | **Templates**, one small file each, parameterised by `Policy` | Policy-driven *boilerplate* whose shape does not vary with the spec |
| Package docs, README, per-op usage examples | **Docs emitter** (`docast`, [docs and mocks](../docs-and-mock.md)), each usage sample returned by the SDK emitter whose call it spells | Prose with holes; a call is spelled by the target that generates it |

A retry loop is the same code for every API; only numbers and header names vary. A typed AST there
buys nothing and costs readability. But a template with an `if` that inspects the IR is a design bug
— that `if` belongs in refine, producing a different node. The boilerplate templates therefore
contain **no policy branching and no structural branching**: all decisions were made in plan/refine;
the template only interpolates scalar policy values (retry count, header name). So templates cannot
diverge across languages the way Mustache *structure* templates do.

## Two guards on the boilerplate templates

Grafted from the template-first stance, applied only to the narrow boilerplate surface:

- **A template lint** (a unit test per pack) parses every `.tmpl` and rejects kind-discriminating
  conditionals, arithmetic, and comparisons beyond boolean presence — only `range`, field access,
  whitelisted pure funcs, and `{{template}}` are allowed. This turns "a template *could* smuggle a
  decision" from a discipline problem into a failing test. A template that renders into a scope
  where spec names live also declares the names it binds there ([reserved
  names](../refine/reserved-names.md)), and the lint renders it and holds that declaration to what
  the rendering binds, so a local added to a template cannot shadow a parameter the declaration left
  out.
- **A mandatory format pass** (`Formatter.Format(path, src)`), run on *all* emitted files. Any
  syntactic slip a template produces is caught here as a located, hard `Diagnostic` (file + the
  formatter's message), never shipped. Because typed-AST structure is already syntactically valid by
  construction, in practice the format pass only ever catches boilerplate-template mistakes.

## Serialization behind a runtime abstraction

The generated *typed veneer* references a `Transport` / `Serializer` interface (Kiota's
`IParseNode`/`ISerializationWriter` seam); the concrete transport is a runtime-policy-configured
library. This is the seam that keeps runtime policy (INV10) out of the typed structural model:
the veneer of models/unions/methods is policy-free, and `Policy` only ever parameterises the
boilerplate that backs the interface.
