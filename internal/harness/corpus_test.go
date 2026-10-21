package harness_test

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
)

// knownInvalid lists the fixtures under testdata/ that a correct compiler
// reports an error diagnostic on under this sweep's default options, so the
// "must be OK" sweep excludes them. Each was read to confirm the error is
// intended behavior, not a compiler bug; its reason sits beside the entry.
// TestHarness_InRepoCorpus fails a listed fixture that is missing or compiles
// clean.
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
		// whose pointer passes through a reference on the chain. The resolver
		// follows each in a loop and reports it as an unresolved reference
		// (docs/parser-library.md#reference-object-cycle, #574).
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
		// components.securitySchemes declaration, dropped with an
		// unresolved-ref error rather than a dangling AuthID (#14).
		filepath.FromSlash("../../testdata/dangling/openapi/f30-protocol-surface.yaml"): true,
		// A $ref whose pointer escapes non-canonically (a raw '~' for a
		// component named "A~B"). The compiler resolves it to the interned
		// node, but the loader reports the malformed JSON pointer as an
		// unresolved-ref error first (#14).
		filepath.FromSlash("../../testdata/dangling/openapi/f32-ref-noncanonical-escape.yaml"): true,
		// The other dangling reproducers (f07, f08, f10, f11, f13, f28, f31)
		// intern their targets and compile clean, so they are absent. f08 and
		// f13 map a tag to a sub-schema that is not a subtype; it resolves as
		// a $ref to it would (#530), and pass.Validate, which this sweep does
		// not run, reports it as pass/discriminator-missing-variant.
	}
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

// TestHarness_InRepoCorpus sweeps every committed spec through all oracles. Any
// non-OK outcome on a spec that is not a known-invalid fixture is a finding; the
// failure message is the full report so the offending specs are named at once.
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

	invalid := knownInvalid()
	seenInvalid := make(map[string]bool, len(invalid))

	var results []harness.Result
	var failures []harness.Result
	for _, r := range swept {
		if invalid[r.Spec] {
			seenInvalid[r.Spec] = true
			assert.NotEqual(t, harness.OutcomeOK, r.Outcome,
				"fixture %s is listed as known-invalid but compiled clean; update knownInvalid", r.Spec)
			continue
		}
		results = append(results, r)
		if r.Outcome != harness.OutcomeOK {
			failures = append(failures, r)
		}
	}

	// Guard the exclusion list against rot: every listed fixture must exist.
	for p := range invalid {
		assert.True(t, seenInvalid[p], "known-invalid fixture %s not found in corpus", p)
	}

	if len(failures) > 0 {
		t.Fatalf("harness findings:\n%s", harness.Report(failures))
	}
	t.Logf("swept %d specs (%d known-invalid excluded), all OK", len(results), len(invalid))
}
