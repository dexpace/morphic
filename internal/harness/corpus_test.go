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

// knownInvalid lists the deliberately-malformed fixtures under testdata/openapi.
// They are excluded from the "must be OK" assertion because an error diagnostic
// on them is correct behavior, not a compiler bug. Each was read to confirm it
// is intentionally invalid before being listed here:
//
//   - resolve_target_invalid.yaml: a response with no description and a header
//     whose required is the string "notabool" — two schema violations.
//   - resolve_main_external.yaml: $refs the malformed target above across files;
//     the compiler does no file I/O, so the external ref surfaces as an
//     unresolved-ref error diagnostic.
func knownInvalid() map[string]bool {
	return map[string]bool{
		filepath.FromSlash("../../testdata/openapi/resolve_target_invalid.yaml"): true,
		filepath.FromSlash("../../testdata/openapi/resolve_main_external.yaml"):  true,
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
