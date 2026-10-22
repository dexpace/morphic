# Ground truth for #136: Finish the compiler decomposition and fix three silent failures

Initial commit reviewed: 861fe727aec90092af9c32d5c0c7b3560bd1627b

Each item below is a commit pushed after the initial one. Most fix a defect a local review found in the initial change; some are planned continuation work and are NOT defects -- judge each.

## 206b23fe
fix(pass): report the group-depth bound and stop it faking findings

forEachGroupOperation stopped at maxGroupDepth and reported nothing, so
every check reaching operations through it walked a subset of the document
while Validate still returned "internally consistent".

It also flipped one check's polarity. checkArgsOutsideGraphQL decides
legality from graphqlReachableTypes, built by the same truncated walk, so
a GraphQL operation nested past the cap went unseen and the types it
legitimises were reported as illegal — a defect that is not there. At
depth 200 the real findings vanish and a false one appears, with nothing
saying the walk stopped early.

Thread truncation out of the walk and report it as ir/walk-truncated, the
code the sibling reference walk already uses, and make the reachability
check fail open when the set it reasons from is incomplete.

## 7b7931b2
fix(compilers/openapi): reject empty preserved payloads, not just nil

preserve screened raw == nil and stopped there. nil and a zero-length
slice are distinct states for a slice, and the second is the worse one:
an empty ir.RawValue is copied into the output byte for byte, so
json.Marshal fails for the entire document and names json.RawMessage
rather than the entry that carried it.

preserveKeyword screens it too rather than delegating, because it
announces the keyword as kept verbatim and would otherwise make that
claim for a payload preserve then discards.

Not reachable from this compiler — every call site re-encodes a parsed
YAML node, which is never zero-length. The hazard is a second compiler
copying an asymmetric guard, so it is closed at the guard.

## 9a72c6f0
feat(compilers): add the compile framework and enforce registry writes

The compiler decomposition's fourth step. compilers/compile owns the two
pieces of state every compiler needs and the invariants they carry: the
type registry with the source-coordinate map that makes invariant 3 true
(stable IDs, one node per coordinate), and diagnostic accumulation with
identity dedup. It imports only ir.

The package boundary is the point. An architecture test asserts no
production package outside compilers/compile constructs or writes an
ir.TypeRegistry, and that rule is inexpressible without an outside —
which is what earns a 190-line package its own directory.

Two shapes fell out of the migration. A composed union variant has no
source coordinate, because the pointer it would occupy already denotes
the branch it was built from, so Register records a minted node without
claiming a coordinate. And NodeAt collapses the resolve-then-fetch pair:
Intern writes coordinate and node together, so a caller doing Lookup then
Node had to branch on a state the type cannot produce — an unreachable
branch that could never be tested.

Behaviour-neutral: every golden is byte-identical and the conformance
suite passes without regeneration.

Document assembly, depth bounding and ID derivation stay with the
compiler. Promoting a borderline item into the framework later is
additive; demoting one breaks every compiler, so they start outside.

## 6213c589
feat(cli): add compile --explain to report one source coordinate

The decomposition's sixth step. --explain reports what compiling produced
at one source pointer: the type node interned there, the coordinates
interned beneath it, and the diagnostics stamped at it.

It answers "why did my example disappear" without widening the compiler
contract. Every hoisted node records its source coordinate in
Provenance.Pointer, so the coordinate-to-node relation is recoverable from
the emitted document alone.

The subtree listing is what makes a miss actionable: a schema that reduced
to a shared primitive owns no node at its own coordinate, and seeing what
did intern below it separates "nothing happened here" from "the node
moved".

What it does not report is which reader filled which field. That needs the
annotation readers to be separable units, which they are not — the facet
extraction was measured and dropped — and claiming it would describe a
compiler this is not.

## c9267b62
test(cli): cover the path boundary in the explain subtree listing

The subtree walk matched on a raw string prefix in the fixture, so a
sibling like /components/schemasOther/X would have been listed under
/components/schemas and no test would have noticed: removing the boundary
left the suite green.

Also gives the ordering fixture IDs that sort opposite to their pointers.
With both orders agreeing, dropping the sort changed nothing and the
assertion passed for the wrong reason.

## 35100322
refactor(compilers/openapi): extract the merge engine behind two seams

The decomposition's fifth step. The eighteen functions that reconcile a
property more than one allOf branch declares — the whole constraint
compatibility lattice, from reconcileProperty down to strConflictDetail —
move to merge.go behind a merger type.

Its only couplings to the walk are the two injected fields: a registry
lookup, because deciding whether two TypeRefs conflict compares what they
point at rather than the references themselves, and a diagnostic recorder.
Both are narrow enough to pass as functions, so the lattice is now
reachable from a test that supplies a map and a slice — no lowerer, no
parser, no spec that happens to produce the pair under test.

fillModelProperties stays in the walk: it recurses through
carriedSchemaRef to build each property, which is walk work. It produces
the declarations; the merger reconciles them.

Behaviour-neutral: every golden byte-identical, conformance passing
without regeneration. schema.go drops from 1601 to 1242 lines.

## 30d55c9e
refactor(compilers/openapi): read site annotations in one place

The decomposition's third step, scoped to what measurement left of it. The
facet extraction was not warranted as specified — the site-versus-referent
rule was already right at 8 of 9 reference sites, and the duplication the
decomposition targeted was elsewhere, fixed as #114. What the measurement
did not dispose of is the reason the step existed: that rule was still
being decided independently at each attachment point.

Three of them decided it separately and disagreed. The property path
passed a referent; the declaration path passed nil, correctly, because a
declaration has none; the parameter path passed nil because it never
resolved the referent it had. Each also read a different subset —
the parameter path read no xml at all.

annotations(site) now reads every site-local aspect once and returns the
values; each carrier writes what it can hold. The site type is the one
the resolver has produced since step 2, whose Kind and Referent fields
had no production reader until now.

The referent stays the walk's to supply. Obtaining one needs reference
resolution, and the two available resolutions are not interchangeable:
site.Referent is one hop, while the property path follows a $ref chain to
its end. Passing the wrong one would silently change default and
description semantics on a ref-to-ref chain.

Behaviour-neutral: every golden byte-identical, conformance passing
without regeneration. The parameter path keeps its nil referent, so #131
stays open — but closing it is now one line here rather than a fourth
independent reading of the rule.

## c96027f3
fix(compilers/compile): refuse registry entries the IR cannot hold

The framework owns the type registry and the invariants it carries, and
shipped with no assertion at its boundary — so it was also the one place
able to manufacture the malformed states irverify and pass.Validate exist
to report. Register("", nil) and an Intern whose build returns a typed nil
both recorded entries that came straight back as ir/nil-type.

Intern and Register now screen an empty coordinate, an empty ID and a nil
type definition, typed or untyped. A refused Intern also unmaps the
coordinate it had already recorded to terminate recursion, since a pointer
resolving to an ID that holds no node is the one state NodeAt's contract
rules out.

Refusing quietly would hide the bug rather than the symptom, so the
refusals accumulate and the compiler reports them as internal-invariant
diagnostics. None is reachable from any source: an empty ID or a nil node
is a compiler bug, which is why the reporting path is driven directly
rather than through a spec.

## 7f801980
fix(pass): report the group-depth cap only when work was skipped

The truncation signal fired one level early. forEachGroupOperation returned
true whenever depth exceeded the cap, and every leaf group recurses once
into its own empty Groups slice — so at exactly maxGroupDepth, where the
last operation still fits and is still visited, Validate reported both the
finding and ir/walk-truncated.

A walk that reached everything must not claim it stopped early: the whole
point of reporting the cap is telling a consumer its results are partial,
and a signal that fires when they are complete makes the honest case
indistinguishable from the truncated one.

Report the cap only when the slice being skipped is non-empty, and pin the
boundary itself — depth 1, 127 and 128 reach the operation and stay quiet;
129 and 200 report truncation and genuinely miss it. The earlier test
checked 127 and 200 and stepped over the off-by-one between them.
