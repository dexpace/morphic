package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
)

// knownInvalid lists the deliberately-malformed fixtures under testdata/ that a
// correct compiler reports an error diagnostic on, so the "must be OK" sweep
// excludes them. An error on these is intended behavior, not a compiler bug.
// Each was read to confirm it is intentionally invalid before being listed here:
//
//   - resolve_target_invalid.yaml: a response with no description and a header
//     whose required is the string "notabool" — two schema violations.
//   - resolve_main_external.yaml: $refs the malformed target above across files;
//     the compiler does no file I/O, so the external ref surfaces as an
//     unresolved-ref error diagnostic.
//   - cycle_self_ref.yaml, cycle_two_node_ref.yaml, their _sibling variants, and
//     cycle_yaml_anchor.yaml: degenerate reference cycles that never reach a
//     concrete schema node. The pre-parse detector reports each as a cyclic-ref
//     error instead of letting the parser fault with a stack overflow (GitHub #12).
//   - dangling/openapi/f04, f05, f06, f08, f09, f13: discriminator mappings whose
//     target is undeclared, external, or a sub-schema, dropped with an
//     unresolved-ref error rather than written as a dangling TypeID (GitHub #14).
//   - dangling/openapi/f12-refs.yaml: a same-file self-reference spelled with the
//     m.yaml basename; swept under its own filename the doc part no longer matches,
//     so the loader reports the external m.yaml it cannot open (GitHub #14).
//   - dangling/openapi/f30-protocol-surface.yaml: a security requirement naming a
//     scheme with no components.securitySchemes declaration, dropped with an
//     unresolved-ref error rather than a dangling AuthID (GitHub #14).
//   - dangling/openapi/f32-ref-noncanonical-escape.yaml: a $ref whose pointer
//     escapes non-canonically (a raw '~' for a component named "A~B"). The compiler
//     resolves it to the interned node (no dangling ID), but the loader still
//     reports the malformed JSON pointer as an unresolved-ref error (GitHub #14).
//
// The remaining dangling reproducers (f07, f10, f11, f28, f31) intern their targets
// and compile clean, so they are deliberately absent — the rot-guard below fails
// any listed fixture that turns out to compile OK.
func knownInvalid() map[string]bool {
	return map[string]bool{
		filepath.FromSlash("../../testdata/openapi/resolve_target_invalid.yaml"):               true,
		filepath.FromSlash("../../testdata/openapi/resolve_main_external.yaml"):                true,
		filepath.FromSlash("../../testdata/openapi/cycle_self_ref.yaml"):                       true,
		filepath.FromSlash("../../testdata/openapi/cycle_self_ref_sibling.yaml"):               true,
		filepath.FromSlash("../../testdata/openapi/cycle_two_node_ref.yaml"):                   true,
		filepath.FromSlash("../../testdata/openapi/cycle_two_node_ref_sibling.yaml"):           true,
		filepath.FromSlash("../../testdata/openapi/cycle_yaml_anchor.yaml"):                    true,
		filepath.FromSlash("../../testdata/dangling/openapi/f04-composition.yaml"):             true,
		filepath.FromSlash("../../testdata/dangling/openapi/f05-discriminator.yaml"):           true,
		filepath.FromSlash("../../testdata/dangling/openapi/f06-discriminator.yaml"):           true,
		filepath.FromSlash("../../testdata/dangling/openapi/f08-discriminator.yaml"):           true,
		filepath.FromSlash("../../testdata/dangling/openapi/f09-discriminator.yaml"):           true,
		filepath.FromSlash("../../testdata/dangling/openapi/f12-refs.yaml"):                    true,
		filepath.FromSlash("../../testdata/dangling/openapi/f13-refs.yaml"):                    true,
		filepath.FromSlash("../../testdata/dangling/openapi/f30-protocol-surface.yaml"):        true,
		filepath.FromSlash("../../testdata/dangling/openapi/f32-ref-noncanonical-escape.yaml"): true,
	}
}

// collectSpecs returns every committed spec under root, excluding golden
// snapshots (they are IR JSON, not input specs).
func collectSpecs(t *testing.T, root string) []string {
	t.Helper()
	var specs []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		if strings.HasSuffix(p, ".golden.json") {
			return nil
		}
		specs = append(specs, p)
		return nil
	})
	require.NoError(t, err)
	return specs
}

// TestHarness_InRepoCorpus sweeps every committed spec through all oracles. Any
// non-OK outcome on a spec that is not a known-invalid fixture is a finding; the
// failure message is the full report so the offending specs are named at once.
func TestHarness_InRepoCorpus(t *testing.T) {
	t.Parallel()
	const root = "../../testdata"
	specs := collectSpecs(t, root)
	require.NotEmpty(t, specs, "corpus sweep found no specs under %s", root)

	invalid := knownInvalid()
	seenInvalid := make(map[string]bool, len(invalid))

	var results []harness.Result
	var failures []harness.Result
	for _, p := range specs {
		data, err := os.ReadFile(p)
		require.NoError(t, err)

		r := harness.Check(context.Background(), p, data)
		if invalid[p] {
			seenInvalid[p] = true
			assert.NotEqual(t, harness.OutcomeOK, r.Outcome,
				"fixture %s is listed as known-invalid but compiled clean; update knownInvalid", p)
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
