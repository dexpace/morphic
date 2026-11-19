// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// specsDir holds the committed specs the compiler's suites share.
const specsDir = "../../../../testdata/openapi"

// conformanceSpecsDir holds the conformance corpus.
const conformanceSpecsDir = "../../../../testdata/conformance/openapi"

// specFrom reads the spec name.yaml of dir.
func specFrom(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name+".yaml"))
	require.NoError(t, err)
	return string(data)
}

// findings spells each diagnostic as its code, severity and pointer, sorted.
func findings(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", diagnostic.Code, diagnostic.Severity, diagnostic.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// tree is v as the JSON it encodes to, in generic maps, slices and scalars, so
// a case reads a field by the name the IR writes it under.
func tree(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v, json.Deterministic(true))
	require.NoError(t, err)
	var out any
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

// valueAtPath follows path from v through maps by key and slices by index, nil where it
// leaves the value.
func valueAtPath(v any, path ...string) any {
	for _, step := range path {
		switch x := v.(type) {
		case map[string]any:
			v = x[step]
		case []any:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// entries spells each entry of the Unmodeled map u, which is the JSON of an
// ir.Unmodeled, as "<reason> <value as JSON> <pointer>", by key.
func entries(t *testing.T, u any) map[string]string {
	t.Helper()
	out := map[string]string{}
	held, _ := u.(map[string]any)
	for key, entry := range held {
		value, err := json.Marshal(valueAtPath(entry, "value"), json.Deterministic(true))
		require.NoError(t, err)
		out[key] = fmt.Sprintf("%v %s %v", valueAtPath(entry, "reason"), value, valueAtPath(entry, "provenance", "pointer"))
	}
	return out
}

// namedItem returns the element of list whose name.source (or wireName) is name.
func namedItem(t *testing.T, list any, name string) any {
	t.Helper()
	items, _ := list.([]any)
	for _, item := range items {
		if valueAtPath(item, "name", "source") == name || valueAtPath(item, "wireName") == name {
			return item
		}
	}
	t.Fatalf("no item named %q in %v", name, list)
	return nil
}

// opTree returns the operation named source as its JSON.
func opTree(t *testing.T, doc *ir.Document, source string) any {
	t.Helper()
	for _, operationGroup := range doc.Services[0].Groups {
		for _, op := range operationGroup.Operations {
			if op.Name.Source == source {
				return tree(t, op)
			}
		}
	}
	t.Fatalf("no operation %q", source)
	return nil
}

// withoutSources is doc's JSON less its source table and diagnostics, which a
// two-order comparison reads apart.
func withoutSources(t *testing.T, doc *ir.Document) string {
	t.Helper()
	stripped := *doc
	stripped.Sources = nil
	stripped.Diagnostics = nil
	out, err := json.Marshal(&stripped, json.Deterministic(true))
	require.NoError(t, err)
	return string(out)
}

// toJSON is v encoded as deterministic JSON text.
func toJSON(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v, json.Deterministic(true))
	require.NoError(t, err)
	return string(out)
}
