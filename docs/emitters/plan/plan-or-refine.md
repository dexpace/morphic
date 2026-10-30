### 3.6 Plan fact or refine fact

The plan classifies and refine decides (§3), and three first targets (§14) test that line from the
first model. A candidate field belongs in the plan only when all three of these hold:

1. **Neutral.** Its value is the same for every target. Ask it concretely: would Go, Kotlin/JVM
   or Kotlin Multiplatform ever want a different value, or would a class-based or dynamic target?
   If any would, it is a refine decision, made by each target with its default injectable through
   the target's options (§2.5).
2. **Closed over the document and plan policy.** It is a function of the IR and `plan.Policy`
   alone, and reads no naming policy, runtime policy, hint or target option. Needing one of those
   is the clearest sign the fact is per-target.
3. **Derived, and exposed by more than one artifact.** Its value is derived rather than read — a
   choice under `plan.Policy`, or a classification or walk over more than one IR fact — and more
   than one artifact generated from the document exposes it, so two of them disagreeing would be a
   defect a user can see: docs describing a response the SDK does not return, a mock paging where
   the SDK's pager does not, an SDK sending a tag value the docs do not list.

A fact the IR states outright fails the third test and gets no field: every consumer reads it from
the IR, and a copy would be a second home for it. A record the plan derives as a whole is not such a
copy, though it holds declared facts: a `PagePlan` the policy may infer has no IR field to read
instead, `Others` and `Errors` collapse several cases into one entry, and the stream and
long-running plans gather facts spread over more than one node — a stream's content and framing, the
poll operation's monitor type — so each reads the same however its facet was written. What the test
removes is a field or record that derives nothing. Any other fact that fails it shapes only the code
of the artifact that uses it — how an untagged union's variant is recognized on decode (§4.4), which
symbols a file imports — and is a shared refine helper in `emitters/refine` when more than one
target needs it: reused code, not a plan field.

The procedure decided every existing field:

1. **Choices under `plan.Policy`** stay: `PrimaryResponse`, `RequestContent` and the content each
   `ResponsePlan` elects, the unwrap behind `Return.Type`, `Open`, and `PagePlan`, an inferred one
   included.
2. **Derivations several artifacts expose** stay: `Return`, `Others`, `Errors`, `Auth`, `Bindings`,
   `Stream` and `LRO`; and `Flattened`, `Required`, `Undeclared`, `Unflattened`, `Union`, `Discrim`,
   `Scalar`, `Shapes`, `Alias` and `Reach`. `Required`, `Undeclared` and `Unflattened` are read
   along the flattening walk (§3.2), `Alias` classifies a decorator over several of its fields and
   resolves its chain (§4.15), and `Reach` walks every operation (§4.17); SDKs and docs that
   disagreed on any of them would mark a member optional, name a field's type or place a type
   differently.
3. **Copies of what the IR states** are gone: whether there is a body, one-way-ness and idempotency
   (§3.1), a model's `Base`, `Implements` and `Mixins`, an enum's openness, value type, flags and
   fallback member, a union's variant list, and the operation-group tree (§3.4). `Union` replaces a
   classification that copied the variant list and a second discriminator beside the tag mode it
   derived, and `Scalar` replaces a bare root that left the constraints it merged nowhere.
   `NeedShapes` is gone too: it listed again the lifecycles `Shapes` is keyed by (§3.3). A client's
   servers are stated by the IR as well, so they get no field (§8.10).
4. **The parameter-passing shape** fails the first test and moved to refine (§4.13): a dynamic
   target's keyword arguments are additive as they stand, where a positional parameter added to a Go
   or Kotlin function breaks its callers, so the best default differs by language.
5. **What the derived-fact index states** gets no field either. How a type is used, whether it is
   recursive and which types share its strongly connected component are `irfacts.Index` facts,
   read through `EmitterInput.Facts`, and a plan copy would be their second home, which is why
   `Usage`, copied into the plan before the index derived it, is gone. Which references of a
   component a target must break fails the first test, since what breaks a cycle — a slice, a
   pointer, a class reference — is the target's (§4.16).

What holds the line beyond review: `emitters/plan` imports only `ir` and `ir/irfacts`, so it cannot
read a naming or runtime policy or a target (§7); the plan is golden-tested once, with no target in
the loop; and a plan field's doc comment names what it derives and which artifacts expose it, which
a reviewer checks against the third test.
