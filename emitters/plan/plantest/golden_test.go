// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package plantest_test holds the plan to golden snapshots over the
// repository's corpus of compiled documents, with no compiler and no target in
// the loop (docs/emitters/plan/overview.md).
package plantest_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/plan"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

const (
	// corpus is the directory, below the repository's root, whose committed
	// IR goldens the plan is built from.
	corpus = "testdata"
	// documentSuffix ends the name of an IR golden, and planSuffix that of the
	// plan golden built from it.
	documentSuffix = ".golden.json"
	planSuffix     = ".plan.golden.json"
	// goldens is the directory the plan goldens live in, laid out as corpus is.
	goldens = "testdata"
)

// corpusDocuments returns the path of every IR golden under corpus, relative
// to it and sorted.
func corpusDocuments(t *testing.T) []string {
	t.Helper()
	root := filepath.Join(irtest.RepoRoot(t), corpus)
	var documents []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, documentSuffix) {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		documents = append(documents, relative)
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, documents, "found no IR golden under %s, so this proves nothing", root)
	slices.Sort(documents)
	return documents
}

// planGolden returns the path of the plan golden built from the IR golden at
// document, a path corpusDocuments returned.
func planGolden(document string) string {
	return filepath.Join(goldens, strings.TrimSuffix(document, documentSuffix)+planSuffix)
}

// TestGolden_PlanOfEveryCorpusDocument builds the plan of every committed IR
// golden under the zero policy and holds it to its own golden, which names the
// document it came from by its path; -update rewrites them. A golden leaves
// the plan's version out, as an IR golden leaves out the irVersion the version
// hashes, so a bump of that rewrites none: TestBuild_VersionIsPinned holds the
// version.
//
// The goldens are then held to the corpus the other way: one whose document is
// gone fails, since no run would compare it again, and -update removes it.
func TestGolden_PlanOfEveryCorpusDocument(t *testing.T) {
	t.Parallel()
	root := filepath.Join(irtest.RepoRoot(t), corpus)
	documents := corpusDocuments(t)
	// Not parallel itself, so it returns once its parallel subtests have: every
	// golden is written before the goldens are listed below.
	t.Run("built", func(t *testing.T) {
		for _, document := range documents {
			t.Run(document, func(t *testing.T) {
				t.Parallel()
				doc, err := irtest.ReadGolden(filepath.Join(root, document))
				require.NoError(t, err)

				built, diags := plan.Build(doc, irfacts.New(doc), plan.Policy{})
				require.Empty(t, diags)
				require.NotEmpty(t, built.Version)
				require.Len(t, built.Types, len(doc.Types), "one entry for each registry type")
				built.Version = ""

				raw, err := json.Marshal(built, jsontext.WithIndent("  "))
				require.NoError(t, err)
				irtest.CompareBytes(t, planGolden(document), append(raw, '\n'))
			})
		}
	})

	t.Run("no golden outlives its document", func(t *testing.T) {
		want := make(map[string]bool, len(documents))
		for _, document := range documents {
			want[planGolden(document)] = true
		}
		for _, golden := range planGoldens(t) {
			if want[golden] {
				continue
			}
			if !irtest.Update() {
				t.Errorf("%s is the plan of no document under %s; -update removes it", golden, root)
				continue
			}
			require.NoError(t, os.Remove(golden))
		}
	})
}

// planGoldens returns the path of every file under goldens.
func planGoldens(t *testing.T) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(goldens, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	require.NoError(t, err)
	return found
}
