// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/internal/harness"
)

// knownInvalid lists the fixtures under testdata/ that a correct compiler or
// pass reports an error diagnostic on under this sweep's default options. Each
// was read to confirm the error is intended behavior, not a bug; its reason
// sits beside the entry. An error stops no oracle, so each is held to every
// other oracle as any fixture is. TestHarness_InRepoCorpus fails a listed
// fixture that is missing or draws no error.
func knownInvalid() map[string]bool {
	return map[string]bool{
		// A response with no description and a header whose required is the
		// string "notabool": two schema violations.
		filepath.FromSlash("../../testdata/openapi/resolve_target_invalid.yaml"): true,
		// $refs the malformed target above across files, so its schema
		// violations are reported as errors.
		filepath.FromSlash("../../testdata/openapi/resolve_main_external.yaml"): true,
		// Degenerate ref cycles that never reach a concrete schema node. The
		// pre-parse detector reports cyclic-ref instead of letting the parser
		// stack-overflow (#12).
		filepath.FromSlash("../../testdata/openapi/cycle_self_ref.yaml"):             true,
		filepath.FromSlash("../../testdata/openapi/cycle_self_ref_sibling.yaml"):     true,
		filepath.FromSlash("../../testdata/openapi/cycle_two_node_ref.yaml"):         true,
		filepath.FromSlash("../../testdata/openapi/cycle_two_node_ref_sibling.yaml"): true,
		filepath.FromSlash("../../testdata/openapi/cycle_yaml_anchor.yaml"):          true,
		// The same degenerate cycle reached via an alias-valued $ref, a $ref
		// under contentSchema, an alias-valued $ref key, a `<<` merge key, and
		// an alias-valued schema node: five shapes the yaml.Node scan must
		// follow (#26).
		filepath.FromSlash("../../testdata/openapi/cycle_alias_ref_value.yaml"):   true,
		filepath.FromSlash("../../testdata/openapi/cycle_content_schema.yaml"):    true,
		filepath.FromSlash("../../testdata/openapi/cycle_alias_ref_key.yaml"):     true,
		filepath.FromSlash("../../testdata/openapi/cycle_merge_key_ref.yaml"):     true,
		filepath.FromSlash("../../testdata/openapi/cycle_alias_schema_node.yaml"): true,
		// A degenerate cycle through one anchored pure-$ref node, reused as a
		// "properties" value and as a schema in its own right. The
		// ref-collection walk keeps a visited set per position, so visiting the
		// node in one does not skip the other (#26).
		filepath.FromSlash("../../testdata/openapi/cycle_alias_dual_position.yaml"): true,
		// A schema map declaring one key twice, only the second declaration
		// cyclic. The resolver works from the last declaration, so the scan
		// must read the mapping last-key-wins too.
		filepath.FromSlash("../../testdata/openapi/cycle_duplicate_key.yaml"): true,
		// Chains of reference objects that cycle, spelled by document position
		// ('#/paths/~1a', '#/webhooks/onA') rather than through components, or
		// whose pointer passes through a reference on the chain: a loop is
		// reported once, at its least member, and a read needing itself as an
		// unresolved reference (docs/compilers/openapi.md#reference-cycles).
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_mutual.yaml"):           true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_self.yaml"):             true,
		filepath.FromSlash("../../testdata/openapi/cycle_webhook_mutual.yaml"):             true,
		filepath.FromSlash("../../testdata/openapi/cycle_response_via_path.yaml"):          true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_via_component.yaml"):    true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_prefix_self.yaml"):      true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_prefix_sibling.yaml"):   true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_prefix_chain.yaml"):     true,
		filepath.FromSlash("../../testdata/openapi/cycle_component_path_item_prefix.yaml"): true,
		filepath.FromSlash("../../testdata/openapi/cycle_webhook_prefix_self.yaml"):        true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_empty_segment.yaml"):    true,
		// A schema self-reference, visible only once the pointer is normalized
		// as the resolver does. Speakeasy trims whitespace around a $ref's
		// pointer, so '#/components/schemas/A ' names A there; a scan of the raw
		// value would call it dangling and miss the cycle.
		filepath.FromSlash("../../testdata/openapi/cycle_pointer_whitespace_self.yaml"): true,
		// A self-reference visible only when a '+' in the pointer is a plus,
		// as RFC 3986 reads it: the resolver reads it as a space and would
		// look for a key that is not there (#911).
		filepath.FromSlash("../../testdata/openapi/cycle_pointer_plus_self.yaml"): true,
		// Schema loops closed by a pointer passing back through a schema $ref on
		// the chain, which the resolver reads by that schema's own members, so it
		// recurses round the loop until the stack runs out (#932).
		filepath.FromSlash("../../testdata/openapi/cycle_schema_through_own_member.yaml"):   true,
		filepath.FromSlash("../../testdata/openapi/cycle_schema_through_member_chain.yaml"): true,
		// Schema loops through a trailing empty token, which the resolver reads
		// in a map of schemas as the key "" and in raw YAML as the node reached,
		// recursing round each until the stack runs out (#944).
		filepath.FromSlash("../../testdata/openapi/cycle_schema_empty_key.yaml"):         true,
		filepath.FromSlash("../../testdata/openapi/cycle_schema_empty_token_stays.yaml"): true,
		// A schema loop under a default response, which outside a schema is a
		// response rather than example data (#949).
		filepath.FromSlash("../../testdata/openapi/cycle_schema_default_response.yaml"): true,
		// A 10-level x 10-way YAML alias fan-out ("billion laughs"). Every
		// alias target is acyclic, so neither the anchor nor the $ref cycle
		// detector catches it, and unguarded it exhausts memory inside
		// soa.Unmarshal before any reference is resolved (#27). The pre-parse
		// scan measures the alias-expanded node count and refuses it.
		filepath.FromSlash("../../testdata/openapi/amplification_alias_bomb.yaml"): true,
		// A request body written as a mapping with a local YAML tag
		// (`!content:`). On a mapping not tagged !!map the parser leaves the
		// request-body model unbuilt and nil-dereferences it on a goroutine the
		// loader's recover cannot reach, so the pre-parse scan refuses the tag
		// first (#474).
		filepath.FromSlash("../../testdata/openapi/tagged_mapping_request_body.yaml"): true,
		// A YAML stream of two OpenAPI documents. The first is lowered, since
		// an OpenAPI document is one YAML document; the second reaches the IR
		// in no form, so it is reported as an error rather than dropped
		// silently (#387).
		filepath.FromSlash("../../testdata/openapi/stream_two_documents.yaml"): true,
		// Discriminator mappings whose target is undeclared or external,
		// dropped with an unresolved-ref error rather than written as a
		// dangling TypeID (#14).
		filepath.FromSlash("../../testdata/dangling/openapi/f04-composition.yaml"):   true,
		filepath.FromSlash("../../testdata/dangling/openapi/f05-discriminator.yaml"): true,
		filepath.FromSlash("../../testdata/dangling/openapi/f06-discriminator.yaml"): true,
		filepath.FromSlash("../../testdata/dangling/openapi/f09-discriminator.yaml"): true,
		// A same-file self-reference spelled with the m.yaml basename. Swept
		// under its own filename the document part no longer matches, so it
		// reads as an external reference, which the default options refuse
		// (#14).
		filepath.FromSlash("../../testdata/dangling/openapi/f12-refs.yaml"): true,
		// A security requirement naming a scheme with no
		// components.securitySchemes declaration, kept with an error beside it,
		// naming a scheme minted for it rather than a dangling AuthID (#14).
		filepath.FromSlash("../../testdata/dangling/openapi/f30-protocol-surface.yaml"): true,
		// The same at every carrier, with a name an entry holds and states no
		// mechanism for, which the specification bars just the same.
		filepath.FromSlash("../../testdata/conformance/openapi/security-unresolved.yaml"): true,
		// A $ref whose pointer escapes non-canonically (a raw '~' for a
		// component named "A~B"). The compiler resolves it to the interned
		// node, but the loader reports the malformed JSON pointer as an
		// unresolved-ref error first (#14).
		filepath.FromSlash("../../testdata/dangling/openapi/f32-ref-noncanonical-escape.yaml"): true,
		// A parameter and a header writing both schema and content, which the
		// specification bars ("but not both"); the case pins how such a document
		// lowers.
		filepath.FromSlash("../../testdata/conformance/openapi/codeclared-schema-content.yaml"): true,
		// A parameter and a header whose content holds several entries, which the
		// specification bars ("MUST only contain one entry"); the case pins how
		// such a document lowers.
		filepath.FromSlash("../../testdata/conformance/openapi/content-several-entries.yaml"): true,
		// Header names no HTTP field can have, which the specification bars: a
		// headers map keyed by what is no token, and a header parameter spelled in
		// two cases; the case pins how such a document lowers.
		filepath.FromSlash("../../testdata/conformance/openapi/header-name-syntax.yaml"): true,
		// An operation binding Idempotency-Key in two cases, a repeat as header
		// names compare in any case; the case pins that the idempotency policy
		// then infers no token.
		filepath.FromSlash("../../testdata/conformance/openapi/idempotency-inference.yaml"): true,
		// Discriminator mappings naming a schema that is not a variant of the
		// base: a sub-schema (f08, f13), a component named "" (f31), and a
		// $defs entry mapped from a base with no composition (defs_at_root,
		// defs_mapping_unreferenced_target). Each pins where a mapping resolves,
		// not polymorphism: the compiler lowers the target as a $ref to it would
		// (#530), and the validate pass reports pass/discriminator-missing-variant.
		filepath.FromSlash("../../testdata/dangling/openapi/f08-discriminator.yaml"):            true,
		filepath.FromSlash("../../testdata/dangling/openapi/f13-refs.yaml"):                     true,
		filepath.FromSlash("../../testdata/dangling/openapi/f31-discriminator-empty-name.yaml"): true,
		filepath.FromSlash("../../testdata/openapi/defs_at_root.yaml"):                          true,
		filepath.FromSlash("../../testdata/openapi/defs_mapping_unreferenced_target.yaml"):      true,
		// The other dangling reproducers (f07, f10, f11, f28) intern their
		// targets and compile clean, so they are absent.
	}
}

// knownFindings lists the findings an oracle other than the error diagnostic
// makes on a committed fixture: a defect in the compiler, each beside the issue
// that holds it. TestHarness_InRepoCorpus fails a fixture whose findings are
// not exactly these, so a fix takes its entry out. An entry names an outcome,
// not what differs, so until then it also stands for any other finding of its
// oracle on that fixture.
func knownFindings() map[string][]harness.Outcome {
	return map[string][]harness.Outcome{
		// A finding on a node that aliases share is located where its anchor is
		// written, which declaration order decides: the library's validation of
		// the node, and the alias-amplification refusal (#1796).
		filepath.FromSlash("../../testdata/openapi/cycle_alias_dual_position.yaml"): {harness.OutcomeOrderDependent},
		filepath.FromSlash("../../testdata/openapi/amplification_alias_bomb.yaml"):  {harness.OutcomeOrderDependent},
	}
}

// knownDeclines lists the fixtures some oracle asks nothing of, each beside the
// one reason every oracle that declines it gives. TestHarness_InRepoCorpus
// fails a decline not listed here and an entry that declines nothing, so an
// oracle never stops reaching a fixture unseen.
func knownDeclines() map[string]harness.Decline {
	return map[string]harness.Decline{
		// Refusals: a fixture the compile lowers to no document leaves the
		// oracles that hold a document, invariants, round trip and idempotence,
		// nothing to hold. Determinism and order-invariance compare the
		// refusals' findings.
		filepath.FromSlash("../../testdata/openapi/amplification_alias_bomb.yaml"):    harness.DeclineNoDocument,
		filepath.FromSlash("../../testdata/openapi/cycle_yaml_anchor.yaml"):           harness.DeclineNoDocument,
		filepath.FromSlash("../../testdata/openapi/tagged_mapping_request_body.yaml"): harness.DeclineNoDocument,
		// Its point is a key written twice, whose reversal changes which
		// declaration wins.
		filepath.FromSlash("../../testdata/openapi/cycle_duplicate_key.yaml"): harness.DeclineDuplicateKey,
	}
}

// knownSequenceDeclines lists the fixtures the sequence-order oracle declines
// though they hold a sequence it would swap, each beside its reason.
// TestHarness_InRepoCorpus holds every other decline of that oracle to
// harness.DeclineNothingToPermute on a fixture holdsOrderFreeSequence finds no
// sequence in, and fails an entry the oracle asks.
func knownSequenceDeclines() map[string]harness.Decline {
	return map[string]harness.Decline{
		// Each allOf lists one alias ten times, beneath an anchor, and what an
		// anchor holds is left as written.
		filepath.FromSlash("../../testdata/openapi/amplification_alias_bomb.yaml"): harness.DeclineNothingToPermute,
	}
}

// holdsOrderFreeSequence reports whether the YAML stream at path writes, at
// any depth of any of its documents, an allOf or required key whose value is a
// sequence of two or more entries. It is deliberately cruder than the oracle's
// own walk, which reads where a mapping holds names rather than keywords, so a
// decline of the oracle is held to a reading it does not share. An alias is
// not followed.
func holdsOrderFreeSequence(t *testing.T, path string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return false
		}
		require.NoError(t, err, "%s must be YAML", path)
		if holdsSequenceBelow(&doc) {
			return true
		}
	}
}

// holdsSequenceBelow is holdsOrderFreeSequence's reading of one node.
func holdsSequenceBelow(n *yaml.Node) bool {
	if n.Kind == yaml.AliasNode {
		return false
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if (key.Value == "allOf" || key.Value == "required") &&
				value.Kind == yaml.SequenceNode && len(value.Content) >= 2 {
				return true
			}
		}
	}
	return slices.ContainsFunc(n.Content, holdsSequenceBelow)
}

// assertSequenceDecline holds a decline of the sequence-order oracle to its
// reason: a listed fixture to the listed one, any other to there being nothing
// to permute, which only a fixture holding no such sequence may say.
func assertSequenceDecline(t *testing.T, spec string, got harness.Decline) {
	t.Helper()
	if want, listed := knownSequenceDeclines()[spec]; listed {
		assert.Equal(t, want, got, "%s: the sequence-order oracle declines it for another reason", spec)
		return
	}
	assert.Equal(t, harness.DeclineNothingToPermute, got, "%s: the sequence-order oracle declines it unlisted", spec)
	assert.False(t, holdsOrderFreeSequence(t, spec),
		"%s writes an allOf or required sequence the sequence-order oracle should have asked of", spec)
}

// fixtureFiles returns every regular file under root that is a fixture rather
// than a snapshot (a name containing ".golden.") or a document (".md"). Names
// that begin with a dot are left out, as the sweep leaves out dot-directories,
// so a file a tool drops beside the fixtures, such as .DS_Store, is not one.
func fixtureFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if p != root && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && !strings.Contains(name, ".golden.") && filepath.Ext(name) != ".md" {
			files = append(files, p)
		}
		return nil
	}))
	return files
}

// TestHarness_InRepoCorpus sweeps every committed spec through all oracles and
// holds each to the findings knownInvalid and knownFindings list for it and to
// the declines knownDeclines lists, failing with the full report so the
// offending specs are named at once. Its log holds the sweep's Summary.
//
// The sweep lists a file only when a registered compiler claims it, so a fixture
// that stops being recognized would drop out of it with every oracle green. It
// also requires every fixture to be listed.
func TestHarness_InRepoCorpus(t *testing.T) {
	t.Parallel()
	const root = "../../testdata"
	swept, err := harness.CheckPath(context.Background(), root)
	require.NoError(t, err)
	require.NotEmpty(t, swept, "corpus sweep found no specs under %s", root)

	listed := make(map[string]bool, len(swept))
	for _, r := range swept {
		listed[r.Spec] = true
	}
	var unlisted []string
	for _, f := range fixtureFiles(t, root) {
		if !listed[f] {
			unlisted = append(unlisted, f)
		}
	}
	assert.Empty(t, unlisted, "fixtures no registered compiler claims are not swept; "+
		"fix the fixture, or if it is deliberately not a spec, move it out of testdata")

	invalid, findings, declines := knownInvalid(), knownFindings(), knownDeclines()
	var unexpected []harness.Result
	for _, r := range swept {
		var want []harness.Outcome
		if invalid[r.Spec] {
			want = append(want, harness.OutcomeErrorDiag)
		}
		want = append(want, findings[r.Spec]...)
		slices.Sort(want)
		if !slices.Equal(want, foundOutcomes(r)) {
			unexpected = append(unexpected, r)
		}
		for _, v := range r.Verdicts {
			switch {
			case v.Asked():
			case v.Oracle == harness.OracleSequenceOrder:
				assertSequenceDecline(t, r.Spec, v.Declined)
			default:
				assert.Equal(t, declines[r.Spec], v.Declined, "%s: %s declines it unlisted", r.Spec, v.Oracle)
			}
		}
	}
	assertEveryEntryNamesASweptSpec(t, listed, slices.Collect(maps.Keys(invalid)), "knownInvalid")
	assertEveryEntryNamesASweptSpec(t, listed, slices.Collect(maps.Keys(findings)), "knownFindings")
	assertEveryEntryNamesASweptSpec(t, listed, slices.Collect(maps.Keys(declines)), "knownDeclines")
	sequenceDeclines := knownSequenceDeclines()
	assertEveryEntryNamesASweptSpec(t, listed, slices.Collect(maps.Keys(sequenceDeclines)), "knownSequenceDeclines")
	// The sequence-order oracle declines nearly every fixture, so a decline of
	// its own says nothing of whether another oracle reaches a listed one.
	declinedBy := func(spec string, wanted func(harness.Oracle) bool) bool {
		return slices.ContainsFunc(swept, func(r harness.Result) bool {
			return r.Spec == spec && slices.ContainsFunc(r.Verdicts, func(v harness.Verdict) bool {
				return !v.Asked() && wanted(v.Oracle)
			})
		})
	}
	for spec := range declines {
		assert.True(t, declinedBy(spec, func(o harness.Oracle) bool { return o != harness.OracleSequenceOrder }),
			"%s is listed in knownDeclines but every oracle asks it its question", spec)
	}
	for spec := range sequenceDeclines {
		assert.True(t, declinedBy(spec, func(o harness.Oracle) bool { return o == harness.OracleSequenceOrder }),
			"%s is listed in knownSequenceDeclines but the sequence-order oracle asks it its question", spec)
	}

	t.Logf("swept %d specs:\n%s", len(swept), harness.Summary(swept))
	if len(unexpected) > 0 {
		t.Fatalf("findings other than knownInvalid and knownFindings list:\n%s", harness.Report(unexpected))
	}
}

// foundOutcomes is what each oracle that found something in r found, sorted,
// so a list need not follow Check's order to match it.
func foundOutcomes(r harness.Result) []harness.Outcome {
	var out []harness.Outcome
	for _, v := range r.Verdicts {
		if v.Found() {
			out = append(out, v.Outcome)
		}
	}
	slices.Sort(out)
	return out
}

// assertEveryEntryNamesASweptSpec guards a hand-written list against rot: an
// entry naming no spec the sweep reached holds nothing.
func assertEveryEntryNamesASweptSpec(t *testing.T, swept map[string]bool, entries []string, list string) {
	t.Helper()
	for _, p := range entries {
		assert.True(t, swept[p], "%s names %s, which the sweep did not reach", list, p)
	}
}
