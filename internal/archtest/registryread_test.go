// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
	"compilers/openapi/internal/schema": {"Building": 1, "Lookup": 1, "Node": 3, "NodeAt": 3},
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

// registryFreeFiles are the files of compilers/openapi/internal/schema that
// decide what a schema admits, or read what decides it, from the source and
// its bindings alone: none names compile.Types, so no decision there turns on
// what a lowering built before it (GitHub #1991). A file listed here that the
// package no longer holds fails too, so the list follows the package.
var registryFreeFiles = []string{
	"admission.go", "admissions.go", "agreement.go", "allof_disjoint.go", "branchshape.go", "keywordkinds.go",
	"nullbranch.go", "nulls.go",
}

// TestRegistryFreeFiles_NameNoRegistry holds each of registryFreeFiles to
// naming compile.Types nowhere: in no expression, parameter or field.
func TestRegistryFreeFiles_NameNoRegistry(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)
	var schema *decls.Package
	for _, p := range pkgs {
		if p.Types.Path() == module+"/compilers/openapi/internal/schema" {
			schema = p
		}
	}
	require.NotNil(t, schema, "the schema package loads")

	held := map[string]bool{}
	for _, f := range schema.Syntax {
		held[filepath.Base(schema.Fset.Position(f.Pos()).Filename)] = true
	}
	for _, name := range registryFreeFiles {
		assert.True(t, held[name], "registryFreeFiles lists %s, which the schema package does not hold", name)
	}
	for expr, tv := range schema.Info.Types {
		at := schema.Fset.Position(expr.Pos())
		if !slices.Contains(registryFreeFiles, filepath.Base(at.Filename)) || !namesRegistry(tv.Type) {
			continue
		}
		t.Errorf("%s names compile.Types, which registryFreeFiles holds its file to naming nowhere", at)
	}
}

// namesRegistry reports whether t is compile.Types, a pointer to it or a
// signature taking or returning one.
func namesRegistry(t types.Type) bool {
	if sig, ok := types.Unalias(t).(*types.Signature); ok {
		for _, list := range []*types.Tuple{sig.Params(), sig.Results()} {
			for v := range list.Variables() {
				if namesRegistry(v.Type()) {
					return true
				}
			}
		}
		return false
	}
	named, ok := pointee(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == module+"/compilers/compile" &&
		named.Obj().Name() == "Types"
}
