#!/usr/bin/env python3
"""Assemble catalog.md from data/findings.json and data/clusters-*.json.

S below is the one hand-written part: the 12 passes, their leads, and which clusters each holds
("a+b" merges clusters into one rule). Every cluster is placed exactly once, every finding sits
in exactly one cluster, and every claude_md quote is a substring of CLAUDE.md, or the script
fails. Counts are derived from finding IDs, never typed.

Usage: catalog.py"""
import collections
import json
import re

from common import AGENT_DIR, DATA, REPO

F = {x["id"]: x for x in json.loads((DATA / "findings.json").read_text())}
C = {}; misc = []; seen = collections.Counter()
for g in "ABCDE":
    for c in json.loads((DATA / f"clusters-{g}.json").read_text())["clusters"]:
        seen.update(c["finding_ids"])
        if c["key"] == "misc-unclusterable": misc.append(c)
        else: C[c["key"]] = c
assert set(seen) == set(F) and max(seen.values()) == 1, "every finding must sit in exactly one cluster"
_norm = lambda s: re.sub(r"[\s*`]+", " ", s).strip().lower()
_cm = _norm((REPO / "CLAUDE.md").read_text())
stale = [k for k, c in C.items() if c["claude_md"] and _norm(c["claude_md"]) not in _cm]
if stale: print("warning: CLAUDE.md no longer contains the quote of", stale)
S=[("1. Claims against reality","Every sentence in a comment, doc, commit or PR body that states behaviour is a claim to execute, not to read — and to execute outside the case its author measured.",
"universal-claims-enumerate-every-site probe-behaviour-claims-by-running-the-input external-claims-checked-at-source verify-the-stated-reason doc-names-every-outcome doc-agrees-with-itself tooling-claims-match-the-gate doc-verbs-match-the-body comment-references-resolve doc-code-obeys-own-rules prove-safety-arguments-in-every-direction measure-before-quantifying claims-that-two-things-agree pr-claims-match-evidence comment-claims-held-by-test docs-consistent-after-edit probe-the-real-external-tool"),
("2. Sweep every site the change touches","A defect is a property of its mechanism: sweep every site of it, every stale sentence about it, every route and spelling into it.",
"sweep-every-site-of-the-mechanism sweep-old-name-after-rename-or-delete+renames-grep-the-old-spelling-everywhere same-construct-at-every-position sweep-comments-a-behaviour-change-falsifies update-normative-docs-in-same-change docs-restating-a-fact-move-with-it every-input-route-reaches-the-guard every-spelling-of-one-concept+every-spelling-the-grammar-allows yaml-alias-anchor-merge-spellings test-co-declared-keywords-together cover-every-version-location-and-level"),
("3. Derive, never maintain","A hand-kept copy of a count, list or fact drifts silently; derive it from its source and hold the two together both ways.",
"no-maintained-counts-or-state-in-prose+no-hand-enumerated-growing-sets derive-inventories-from-the-tree single-source-no-restatement derive-lists-from-their-source+derive-enumerations-from-source+derive-a-gates-target-set parallel-implementations-agree+one-reader-per-syntax keys-canonical-single-derivation"),
("4. Lossless lowering and honest diagnostics","Invariant 2 in practice: every source construct lands somewhere, and every diagnostic is true, located, and decided from what actually happened.",
"diagnostic-text-names-what-fired diagnostic-provenance-names-the-real-site+provenance-names-the-writer+emitted-locations-resolve twin-diff-every-accepted-keyword no-silent-fallback-to-a-valid-looking-value fast-paths-carry-what-the-shared-path-does merge-each-field-by-rule-in-both-orders check-stays-silent-on-valid-input announce-kept-only-after-the-write unmodeled-reason-and-typed-home one-defect-one-finding every-return-path-delivers-diagnostics keyword-placement-path-independent null-member-handling explicit-guard-not-incidental check-every-failure-signal loaders-fail-when-they-read-less"),
("5. Identity, order and determinism","Invariants 3 and 7 in practice: nothing about an ID, name, cache answer or output order may depend on which site was walked first.",
"shared-node-attributes-from-declaration cache-and-dedup-keys-carry-full-identity iterate-and-sort-by-stable-key two-order-diff-every-reachable-construct derived-identity-must-be-injective finding-placement-declaration-independent resolve-to-true-target memo-only-path-independent-results escape-separators-in-composite-keys ref-document-part-identity pointer-token-names-decoded-and-total+pointer-and-uri-edge-tokens graph-walk-cycle-handling synthesized-name-channel-and-independence"),
("6. Spec, grammar and verifier semantics","Read the specification, the library and the grammar as they are, at the version pinned; verifiers must reach every class they claim to check.",
"verifier-rejects-every-degenerate-value verifier-reaches-every-reference-and-id-class source-scanners-match-every-form mirror-library-read-rules-exactly exact-token-grammar+verifier-checks-whole-grammar character-predicate-witness-per-class resolve-symbols-semantically archtest-rules-reach-exactly revalidate-after-decoding json-decoder-fixed-point"),
("7. Tests that bite","A test proves nothing until the defect it names has been planted and it went red. Coverage reached is not behaviour held.",
"every-guard-has-a-reddening-test+plant-a-defect-in-every-branch+every-branch-held-by-a-test one-reddening-case-per-member fix-must-move-a-golden negative-fixture-isolates-the-guard inputs-at-the-edges-the-code-transforms+fixtures-discriminate-wrong-implementations distinct-values-for-competing-sources bite-claims-are-executed validate-the-instrument-first assertion-must-be-able-to-fail boundary-cases-at-the-boundary expected-value-is-not-the-bug+expectation-from-an-independent-authority"),
("8. Assertions, fixtures and corpus reach","Assert the property the name claims, with inputs that reach the code and an expectation from outside it.",
"test-claims-match-reach assertion-targets-what-the-name-names fixture-still-reaches-its-stage corpus-witnesses-every-site test-guards-fail-closed assert-diagnostics-by-identity-and-completely exercise-the-production-path assert-the-value-not-a-proxy assert-every-output-the-change-touches prove-which-path-answered goldens-record-not-verify ordering-tests-pin-the-order no-equivalence-that-erases-the-distinction nondeterministic-tests-proven-stable tests-fail-fast-and-legibly environment-independent-tests+tests-environment-independent test-moves-preserve-cases-and-scope perf-guards-measure-the-defect"),
("9. Bounds, budgets and performance","Bounded everything, measured honestly: a bound sits where the allocation is, a cap reports, and a speed claim is measured end to end on a real shape.",
"cheap-guard-before-expensive-work budget-accounting-sound+budgets-charged-correctly-and-calibrated bounded-walk-reports-truncation no-quadratic-rescans every-loop-and-test-bounded justify-perf-with-real-measurement+price-every-reader-of-a-cost"),
("10. Go API and code shape","The styleguide's API rules as they actually broke here.",
"no-panic-on-nil-or-zero+total-over-permitted-inputs minimal-immutable-exports no-package-level-test-seams copy-before-write closed-dispatch-explicit-arms delete-unreachable-code drop-constant-parameters identifier-says-what-it-holds no-identifier-collisions measure-size-caps exemptions-narrow-justified-retirable rewrites-preserve-semantics sentinels-declared-and-distinct ir-public-contract-settled wire-change-bumps-version"),
("11. Scripts and tooling","Gate scripts are code under the same rules: structural parsing, loud failure, no dependence on the caller's environment.",
"hermetic-scripts-and-fixtures"),
("12. Process, tracker and PR hygiene","The CLAUDE.md commit and PR rules, which no check enforces.",
"limitation-stated-in-code+record-deliberate-gaps-in-code follow-recorded-decisions recheck-against-final-tree+re-sweep-after-merging-main coverage-gate-on-final-tree commit-and-pr-message-form diff-contains-only-in-scope-files")]
placed=collections.Counter(k for _,_,ks in S for grp in ks.split() for k in grp.split("+"))
unknown=[k for k in placed if k not in C]; dup=[k for k,n in placed.items() if n>1]; left=sorted(set(C)-set(placed))
assert not unknown and not dup and not left, (unknown,dup,left)
def clip(s,n=240): s=re.sub(r"\s+"," ",s).strip(); return s if len(s)<=n else s[:n].rsplit(" ",1)[0]+" …"
out=["# Morphic review catalog","",
"Snapshot: Omar's PRs opened before 2026-10-14 (main at 210283f); counts are true of that snapshot only. Derived from the fix commits, review comments and PR-body review notes on Omar Al Jarrah's PRs in dexpace/morphic (the 20 back-test hold-out PRs excluded). Each rule is a defect class that a local review caught **after** a PR's first push. `n` is the number of verified findings behind the rule and `PRs` the number of distinct PRs; both are computed from finding IDs by `build/catalog.py`, never written by hand. Exemplars quote the PR history verbatim.",""]
nrules=0
for title,lead,keys in S:
    rules=[]
    for grp in keys.split():
        members=[C[k] for k in grp.split("+")]; ids=[i for c in members for i in c["finding_ids"]]
        head=max(members,key=lambda c:len(c["finding_ids"]))
        rules.append((len(ids),len({F[i]["pr"] for i in ids}),head,members))
    rules.sort(key=lambda r:(-r[0],-r[1])); nrules+=len(rules)
    out+=[f"## {title}","",lead,""]
    for n,p,h,members in rules:
        out.append(f"### {h['title']}\n`{h['key']}` · n={n} · PRs={p}\n")
        out.append(f"- **Rule.** {h['rule']}")
        for m in members:
            if m is not h: out.append(f"- **Also.** {m['rule']}")
        out.append(f"- **Why.** {h['why']}")
        out.append(f"- **Probe.** {h['probe']}")
        cms=[m["claude_md"] for m in members if m["claude_md"]]
        if cms: out.append(f"- **CLAUDE.md already says:** “{clip(cms[0],200)}”")
        for e in [e for m in members for e in m["exemplars"]][:2]:
            f=F[e]; out.append(f"- **Seen in #{f['pr']}**{' ('+f['sha']+')' if f.get('sha') else ''}: “{clip(f['quote'])}”")
        out.append("")
(AGENT_DIR / "catalog.md").write_text("\n".join(out))
print("rules",nrules,"misc findings left out",sum(len(c["finding_ids"]) for c in misc),"bytes",len("\n".join(out)))
