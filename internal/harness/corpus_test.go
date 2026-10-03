package harness_test

import (
	"context"
	"path/filepath"
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
		// $refs the malformed target above across files, and external refs are
		// refused by default, so it is an unresolved-ref error.
		filepath.FromSlash("../../testdata/openapi/resolve_main_external.yaml"): true,
		// Well-formed, unlike every other entry here. Both $ref a sibling
		// document, which the compiler opens only under AllowExternalRefs; the
		// sweep leaves it off (#31). Tests of their cross-document lowering opt
		// in. They compile clean, and come off this list, once external targets
		// load as sources (#74).
		filepath.FromSlash("../../testdata/openapi/resolve_main_external_valid.yaml"):       true,
		filepath.FromSlash("../../testdata/openapi/resolve_main_alias_external_valid.yaml"): true,
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
		// Reference-object cycles spelled by document position ('#/paths/~1a',
		// '#/webhooks/onA') rather than through components. Speakeasy guards
		// the components spelling and faults on these, so the pre-parse scan
		// refuses them too.
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_mutual.yaml"):        true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_self.yaml"):          true,
		filepath.FromSlash("../../testdata/openapi/cycle_webhook_mutual.yaml"):          true,
		filepath.FromSlash("../../testdata/openapi/cycle_response_via_path.yaml"):       true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_via_component.yaml"): true,
		// A $ref whose pointer passes *through* a reference already being
		// resolved. Speakeasy resolves a reference holding its own write lock
		// and read-locks every reference the pointer walk traverses, so
		// re-entering one deadlocks on a non-reentrant RWMutex before the hop
		// completes and its own cycle guard can run. Unlike the cycles above,
		// the components spelling deadlocks too, so all spellings are refused.
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_prefix_self.yaml"):      true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_prefix_sibling.yaml"):   true,
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_prefix_chain.yaml"):     true,
		filepath.FromSlash("../../testdata/openapi/cycle_component_path_item_prefix.yaml"): true,
		filepath.FromSlash("../../testdata/openapi/cycle_webhook_prefix_self.yaml"):        true,
		// The same re-entrant prefix spelled with a trailing separator. The
		// empty token it ends in names the key "" under the path item, so the
		// resolver descends through the reference it is already resolving; a
		// pointer walk that dropped the token would let it past.
		filepath.FromSlash("../../testdata/openapi/cycle_path_item_empty_segment.yaml"): true,
		// The same self-reference, visible only once the pointer is normalized
		// as the resolver does. Speakeasy trims whitespace around a $ref's
		// pointer, so '#/paths/~1a ' names /a there; a scan of the raw value
		// would call it dangling and miss the cycle.
		filepath.FromSlash("../../testdata/openapi/cycle_pointer_whitespace_self.yaml"): true,
		// A 10-level x 10-way YAML alias fan-out ("billion laughs"). Every
		// alias target is acyclic, so neither the anchor nor the $ref cycle
		// detector catches it, and unguarded it exhausts memory inside
		// soa.Unmarshal before ResolveAllReferences runs (#27). The pre-parse
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

// TestHarness_InRepoCorpus sweeps every committed spec through all oracles. Any
// non-OK outcome on a spec that is not a known-invalid fixture is a finding; the
// failure message is the full report so the offending specs are named at once.
func TestHarness_InRepoCorpus(t *testing.T) {
	t.Parallel()
	const root = "../../testdata"
	swept, err := harness.CheckPath(context.Background(), root)
	require.NoError(t, err)
	require.NotEmpty(t, swept, "corpus sweep found no specs under %s", root)

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
