# Open questions (tracked for implementation)

1. **Plan-fact vs refine-fact boundary.** Settled in [plan fact or refine
   fact](plan/plan-or-refine.md), before the plan layer is built, since three first targets test it
   from M3a. It moved the parameter-passing shape out of the plan and removed the fields that copied
   what the IR states.
2. **Target-AST granularity.** The line is "declarations and signatures = typed AST; behavior and
   boilerplate = template". Method *bodies* that are pure boilerplate (the pager's fetch loop, the
   retry loop) stay templated; an over-fine AST that models statement-level Go grammar is a
   non-goal. The exact granularity is a per-feature judgment that will be litigated as features land.
3. **A target's own content preference.** The `json > form > multipart > binary` default is a shared
   plan helper (resolves [open question 3](../ir/open-questions.md)), and [one
   election](plan/responses.md#one-election) settles how it breaks a tie, but a target that wants a
   different primary needs a clean override path that does not fork the plan. Leaning: an override
   on `plan.Policy`, since the choice is document-neutral.
4. **Protocol-view neutrality.** The `BindingView` and target-AST `MethodKind` must be designed
   against the full binding matrix (HTTP/RPC/message/GraphQL/OTP) from day one, or they ossify
   around HTTP the way Kiota's model did. The first targets' traces are HTTP-heavy; the
   RPC/messaging targets are the real test. Revisit when the second-protocol emitter lands (tracks
   [open question 6](../ir/open-questions.md)).
5. **Manifest granularity for merge.** Whether the entity→symbol map keys at property level (not just
   type/op level) to support field-level additive merge, or leaves sub-type merge to the `go/ast`
   adapter. Decide when write/integrate (M4) is built.
6. **Surface behavioral channel scope.** Which behavioral changes beyond default-value changes
   (e.g. a `Retryable` flip, an idempotency change) belong in the separate behavioral channel vs the
   structural one. Decide when surface verification (M4) is built against real cross-revision diffs.

7. **Behavior server distribution.** The server T-14 runs against is Go in this repository, and a
   target in another language needs it running beside its own test suite. Whether that is a
   released binary, a container image or a build at a pinned revision is decided when the Kotlin
   targets' T-14 suite is built (M3c).
8. **Kotlin's unit size.** The [size check](refine/output-units.md#the-size-check) needs a default
   limit on a Gradle module's declarations for each Kotlin target, as Go's is set from a measured
   build. It is set by measuring kotlinc and the Kotlin/Native framework build when the Kotlin
   targets are built (M3a), and moves only with an options version, as every default does ([target
   options](contract/options.md)).
