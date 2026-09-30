# Task: fix irverify value pointer cycles — #573

Repo `dexpace/morphic`, base `main` @ `07576ab`. Your worktree branch is `relevo/irverify-cycles`; rename it to `fix/irverify-cycles` before pushing. This is PR B of a two-PR batch; PR A owns `ir/irverify/naming.go`, `irverify.go` and the message-quoting sites, which are OFF LIMITS here. Read `CLAUDE.md` first — its invariants, the 100%-coverage gate and the "verify by executing" rules bind this work. The plan commits as `docs/plans/573-irverify-cycles.md`.

## MasterMind decision

A slice whose element holds the same slice (`l[0].List = l`, likewise through `Value.Object` or `CtorValue.Args`) is a second cycle mechanism that involves no pointer. It hangs `Verify` (exponential to `MaxWalkDepth`) and `json.Marshal` refuses it. That mechanism is out of scope here: it needs `ir.WalkValues` itself and lands on the `checkNaming` walk that runs before `checkValues`. You draft the follow-up issue (below) and record the limit in your doc comment.

## Behaviour

**Today.** `checkValues` (`ir/irverify/values.go:58-67`) relies on `WalkValues`' global `seen` (`ir/walk.go:144-154`). A repeated pointer is not descended into, so the #573 repro gives `Verify → []` while `json.Marshal` fails.

**After.**
- `checkValues`' visitor also tracks every non-nil `reflect.Pointer` it is handed. `WalkValues` calls the visitor on a pointer *before* its `seen` check (`ir/walk.go:91-98`), so the visitor sees every meeting.
- It keeps its own on-path stack of (pointer type, address, path). On each visit it pops entries whose path is not an ancestor of the current one: equal, or a prefix followed by `.` or `[`. The stack is bounded by the walk depth `ir.MaxWalkDepth`. No runtime guard is added — it would be an uncoverable statement (the `naming.go:104-109` precedent).
- Meeting a pointer already on the stack reports **`ir/value-cycle`** at the current walk path. For the repro: `doc.Types[t/x/M].Examples[0].Value.Ctor.Args[0].Ctor`. Message: `value reaches a pointer it is already inside (entered at <Quote(ancestor path)>), so the document cannot be encoded`.
- A pointer met again off the path (shared but acyclic) stays clean.
- The check is generic over pointers, so a future self-reachable pointer is held the moment it exists. The executable survey (step 3) says `*ir.CtorValue` is the only one today.
- **Limits recorded in the doc comment:** the ancestor test reads paths, so a map key containing `.` or `[` could make a sibling look like a descendant (only when that key also shares a pointer); an embedded pointer field would share its owner's path (the document graph has none); slice-alias cycles are out of scope, referencing the follow-up.

## Draft follow-up issue (open it before `gh pr create`, then link it in the PR body)

- **Title:** `ir: WalkValues does not guard slice aliasing, so a self-containing list hangs Verify`
- **Body:** `l := make([]ir.Value, 2); l[0] = ir.Value{Kind: ir.ValueList, List: l}; l[1] = l[0]` placed in a model example makes `irverify.Verify` run exponentially up to `MaxWalkDepth` — it was still running after 5 s. With one element it reports only `ir/walk-truncated`, and `json.Marshal` refuses both with "encountered a cycle". The `seen` guard (`ir/walk.go:144-154`) covers pointers only. `[]ir.Value` and `[]ir.Field` are self-reachable (`Value.List`, `Value.Object`, `CtorValue.Args`). The fix belongs in `ir.WalkValues`: guard slice backing ranges, then report the cycle in irverify as #573 does for pointers.

## Seams

- `ir/irverify/values.go`: `checkValues` (58-67) and its doc comment (44-57); a small helper for the on-path stack and the ancestor test.
- `ir/irverify/values_test.go`; `values_internal_test.go` only if the survey needs package access (it does not — it uses exported `ir` types).
- **Off limits:** `irverify.go`, `naming.go`, `ir/walk.go`, and everything PR A touches.
- No golden changes: no compiler produces a `CtorValue` cycle, as #573 says.

## Ordered steps (each with its check)

1. **Repro test.** Add `TestVerify_ValueCycleIsAViolation` with the exact #573 document: one violation, code `ir/value-cycle`, the path above, message naming the quoted ancestor `…Examples[0].Value.Ctor`. Check: it reddens against `main` with `go test ./ir/irverify -run ValueCycle -count=1`.
2. **Implement the check.** Add the on-path stack to `checkValues` and update the doc comment. Add `TestVerify_SharedAcyclicPointerIsClean` (one `*CtorValue` in two examples → empty) and `TestVerify_ValueCyclePathIsSpelledAsTheWalkWould` (collect the paths at which `ir.WalkValues` hands over that `*CtorValue`; the violation path is the second). `TestVerify_AliasedPointerIsDeterministic` (`refs_test.go:272`) stays green as a second shared control. Check: `go test ./ir/irverify -run 'Value|Aliased' -count=1`.
3. **Executable survey.** Add `TestValueCycle_OnlyCtorValueReachesItself`: reflection over the types reachable from `*ir.Document`, expanding `ir.TypeDef` to its implementers. Assert the self-reachable pointer set is `{*ir.CtorValue}` and the self-reachable slice set is `{[]ir.Value, []ir.Field}`. A new entry reddens and forces a review of the code name and the follow-up. Check: same command.
4. **Mutation probes.** Make the on-path test always false → `ValueCycleIsAViolation` reddens; restore. Make it always true (any repeat counts) → `SharedAcyclicPointerIsClean` reddens; restore, confirm green.
5. **Corpus and conformance.** `go test ./ir/irverify -run TestVerify_Corpus -count=1` and `go test ./compilers/openapi -run TestConformance -count=1`. Expect no change.
6. **Gate.** `make gate` exits 0 at 100% coverage.

   | Arm | Covering test |
   |---|---|
   | Non-pointer, nil pointer | every existing test (`Ref`/`Ctor` nil) |
   | Push | `SharedAcyclicPointerIsClean` |
   | Pop / trim | `SharedAcyclicPointerIsClean` (the second example is a sibling path) |
   | Off-path repeat | `SharedAcyclicPointerIsClean` |
   | On-path repeat | `ValueCycleIsAViolation` |

7. **Ship.**
   - Rename the worktree branch to `fix/irverify-cycles`; commit `docs/plans/573-irverify-cycles.md` (this plan) first, then the change.
   - PR title: `fix(irverify): report a value pointer cycle` — 50 chars; verify `printf '%s (#999)' "<title>" | wc -c` ≤ 72.
   - PR body: Summary / Test plan, ending `Closes #573`. Name the slice-alias follow-up and note that #539's "verifies clean ⇒ round-trips" now holds for pointer cycles only.
   - `gh pr create --assignee fuad-daoud --reviewer OmarAlJarrah`. Push the branch.

## Deleted behaviour (closed list)

1. A document whose values reach a pointer they are already inside no longer verifies clean.

Nothing else is removed. Shared-but-acyclic pointers still verify clean.

## Report must include

- Changed paths with line ranges; the commands run and their outcomes.
- The golden status (none expected).
- The mutation probes, each with the test that reddened and the green result after restoring.
- The survey output: pointer set and slice set.
- The settled code `ir/value-cycle`.
- The follow-up issue URL.
- Branch, PR URL, measured title length.

**Commands:** focused `go test ./ir/irverify -run '<pattern>' -count=1`; corpus `go test ./ir/irverify -run TestVerify_Corpus -count=1`; conformance `go test ./compilers/openapi -run TestConformance -count=1`; full `make gate`.