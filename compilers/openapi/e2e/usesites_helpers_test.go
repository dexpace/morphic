// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

// compileVerified compiles src as the file name through the engine, as
// `morphic compile --verify` does: every document the compiler and each pass
// return is held to the IR's invariants, and a breach is a Go error. It returns
// the document and every finding of the run, the passes' beside the compiler's.
func compileVerified(t *testing.T, name, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return runVerified(t, compilers.Source{Path: name, Data: []byte(src)}, nil)
}

// compileVerifiedOnDisk is compileVerified for a root document written under
// dir that reads another document beside it.
func compileVerifiedOnDisk(t *testing.T, dir, root string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, root))
	require.NoError(t, err)
	return runVerified(t, compilers.Source{Path: filepath.Join(dir, root), Data: data},
		openapi.Options{AllowExternalRefs: true})
}

// runVerified runs src through a verified engine run under the format options
// opts, nil for the defaults.
func runVerified(t *testing.T, src compilers.Source, opts any) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	e, err := engine.New()
	require.NoError(t, err)
	res, err := e.RunSpec(t.Context(), src, engine.RunOptions{Verify: true, FormatOptions: opts})
	require.NoError(t, err, "the run must verify: every stage's document holds the IR's invariants")
	require.NotNil(t, res.Document)
	return res.Document, res.Diagnostics
}

// corpusFile reads one spec of the conformance corpus by its file name.
func corpusFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
	require.NoError(t, err)
	return string(data)
}

// findings spells each diagnostic as its code, severity and pointer, sorted, so
// a case states the whole list a document draws without its message text or
// the order the lowerings report in.
func findings(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Code, d.Severity, d.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// tree is v as the JSON it encodes to, in generic maps, slices and scalars, so
// a case reads a field by the name the IR writes it under, where the field
// itself may not be declared yet.
func tree(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v, json.Deterministic(true))
	require.NoError(t, err)
	var out any
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

// at follows path from v through maps by key and slices by index, and returns
// nil where the path leaves the value.
func at(v any, path ...string) any {
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

// keptValues returns the value of each entry of the Unmodeled map u, which is
// the JSON of an ir.Unmodeled, by key.
func keptValues(u any) map[string]any {
	out := map[string]any{}
	entries, _ := u.(map[string]any)
	for key, entry := range entries {
		out[key] = at(entry, "value")
	}
	return out
}

// keptReasons returns the reason of each entry of the Unmodeled map u by key.
func keptReasons(u any) map[string]any {
	out := map[string]any{}
	entries, _ := u.(map[string]any)
	for key, entry := range entries {
		out[key] = at(entry, "reason")
	}
	return out
}

// keptPointers returns the source pointer of each entry of the Unmodeled map u
// by key.
func keptPointers(u any) map[string]any {
	out := map[string]any{}
	entries, _ := u.(map[string]any)
	for key, entry := range entries {
		out[key] = at(entry, "provenance", "pointer")
	}
	return out
}

// namedItem returns the element of list whose name.source is name.
func namedItem(t *testing.T, list any, name string) any {
	t.Helper()
	items, _ := list.([]any)
	for _, item := range items {
		if at(item, "name", "source") == name {
			return item
		}
	}
	t.Fatalf("no item named %q in %v", name, list)
	return nil
}

// opTree returns the operation whose operationId is source, as its JSON.
func opTree(t *testing.T, doc *ir.Document, source string) any {
	t.Helper()
	op, ok := opByName(doc, source)
	require.True(t, ok, "operation %q", source)
	return tree(t, op)
}

// groupTree returns the operation group with the ID id, as its JSON, found
// through every service and nested group.
func groupTree(t *testing.T, doc *ir.Document, id string) (any, bool) {
	t.Helper()
	var find func(groups []ir.OperationGroup) (ir.OperationGroup, bool)
	find = func(groups []ir.OperationGroup) (ir.OperationGroup, bool) {
		for _, g := range groups {
			if string(g.ID) == id {
				return g, true
			}
			if got, ok := find(g.Groups); ok {
				return got, true
			}
		}
		return ir.OperationGroup{}, false
	}
	for _, svc := range doc.Services {
		if g, ok := find(svc.Groups); ok {
			return tree(t, g), true
		}
	}
	return nil, false
}

// testdataSpec reads one spec of testdata/openapi by its file name.
func testdataSpec(t *testing.T, name string) string {
	t.Helper()
	return specFile(t, name+".yaml")
}
