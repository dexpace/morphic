// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir/irtest"
)

// registryReadBaseline is how many times each production package, keyed by
// its repo-relative path, asks the type registry (compile.Types) what a
// lowering has built so far, by method. TestRegistryReads_HeldToTheBaseline
// fails when a count rises, since each new read makes a decision turn on
// which lowering ran first, and when one falls until the count here is
// lowered to match, so the map only shrinks as decisions move onto what the
// source states (GitHub #1991).
var registryReadBaseline = map[string]map[string]int{
	"compilers/openapi/internal/schema": {"Building": 1, "Lookup": 8, "Node": 6, "NodeAt": 4},
}

// registryReadRatchet is how a difference from registryReadBaseline reads.
var registryReadRatchet = ratchet{
	name:   "registryReadBaseline",
	counts: registryReadBaseline,
	what:   "reads of compile.Types",
	rise:   "each makes a decision turn on what a lowering built before it (GitHub #1991)",
}

// registryReads names the registry's methods that read what was built.
var registryReads = []string{"Lookup", "NodeAt", "Node", "Building"}

// registrySelections returns where p calls or names one of registryReads, by
// method.
func registrySelections(p *decls.Package) map[string][]string {
	out := map[string][]string{}
	for expr, sel := range p.Info.Selections {
		fn, ok := sel.Obj().(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != module+"/compilers/compile" ||
			!slices.Contains(registryReads, fn.Name()) {
			continue
		}
		recv := fn.Signature().Recv()
		if recv == nil || pointee(recv.Type()).(*types.Named).Obj().Name() != "Types" {
			continue
		}
		out[fn.Name()] = append(out[fn.Name()], p.Fset.Position(expr.Sel.Pos()).String())
	}
	return out
}

// TestRegistryReads_HeldToTheBaseline holds every production package's reads
// of what the type registry has built to registryReadBaseline.
func TestRegistryReads_HeldToTheBaseline(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)
	root := irtest.RepoRoot(t)

	sites := map[string]map[string][]string{}
	for _, p := range pkgs {
		rel := strings.TrimPrefix(p.Types.Path(), module+"/")
		for method, at := range registrySelections(p) {
			if sites[rel] == nil {
				sites[rel] = map[string][]string{}
			}
			slices.Sort(at)
			for _, pos := range at {
				sites[rel][method] = append(sites[rel][method], strings.TrimPrefix(pos, root+"/"))
			}
		}
	}
	for _, finding := range baselineFindings(registryReadRatchet, sites) {
		t.Error(finding)
	}
}
