# Unmodeled entries

An `Unmodeled` entry is a construct the IR keeps verbatim rather than modeling it (ir-design §12).
An SDK emitter acts on an entry's `Reason`, never on its key, and reports none of them:

| Reason | What an SDK emitter does |
|---|---|
| `vendor_extension` | nothing, unless a policy names the key ([runtime policy](../contract/runtime-policy.md)) |
| `out_of_scope` | nothing, unless a policy names the key: emitter policy is the consumer the IR keeps these for |
| `validation_only` | nothing; a validation emitter reads them ([docs and mocks](../docs-and-mock.md)) |
| `degraded_lowering` | lowers the weaker shape the IR holds beside the entry, never the entry |
| `no_ir_home` | nothing: honouring the construct waits for the field the IR lacks, never a key read by name |

**Reporting is the compiler's.** The compiler is the stage that chose to keep a construct rather
than model it, and its specification states the diagnostic each kept construct draws, or that it
draws none (ir-design §12). `Document.Diagnostics` carries those reports to every consumer, so a
report from the emitter would repeat each one once per target and add only "this target ignores
it", which the table already says of every SDK target.

**Keys are read by name only through a policy.** An SDK emitter reads an entry's key only where a
policy names it, and only `vendor_extension` and `out_of_scope` entries are kept for policies
(INV6), so the stability of key names (#429) reaches an SDK through those two reasons alone. The
validation emitter reads `validation_only` keys as the keywords they are ([docs and
mocks](../docs-and-mock.md)), which #429 covers too.

The copies of `readOnly` and `default` that the OpenAPI compiler keeps where they were declared,
after applying them at every use
([`use-bound-annotations`](../../compilers/openapi.md#use-bound-annotations)), need no rule of their
own: every use site already carries the fact, and an SDK reads no `no_ir_home` entry.
[T-15](../testing/mandatory-tests.md) holds the table.
