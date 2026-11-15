// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
)

// moduleRoot is the repository root, which is four directories above this
// package.
const moduleRoot = "../../../.."

// exportedListVar matches the declaration the keyword lists used to take: an
// exported package-level slice or map, which any importer can mutate and every
// other importer then reads.
var exportedListVar = regexp.MustCompile(`^var [A-Z][A-Za-z0-9]* = (\[\]|map\[)`)

// TestKeywordLists_AreNotExported holds the module to no exported package-level
// slice or map in a production file. A list a package exports is one value
// every importer shares, so a caller that sorts or appends to it changes what
// every other reader sees (GitHub #1316).
func TestKeywordLists_AreNotExported(t *testing.T) {
	t.Parallel()
	var found []string
	var scanned int
	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "node_modules", ".claude":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		for line := range strings.SplitSeq(string(data), "\n") {
			if exportedListVar.MatchString(line) {
				found = append(found, filepath.ToSlash(path)+": "+line)
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, scanned, 100, "the walk reached the module's production files")

	assert.Empty(t, found, "a package exposes its list through a function, not a shared variable")
}

// TestDialectKeywords_YieldsTheDialectKeywords pins the members the seam gives
// out, so a mutation test below has something to mutate.
func TestDialectKeywords_YieldsTheDialectKeywords(t *testing.T) {
	t.Parallel()
	assert.ElementsMatch(t, []string{"$id", "$schema", "$vocabulary"},
		slices.Collect(annotation.DialectKeywords()))
}

// TestDialectKeywords_EachReaderGetsItsOwnCopy mutates through one reader's
// result and holds another's to what it was: the seam yields values, so no
// reader holds the list another one can change.
func TestDialectKeywords_EachReaderGetsItsOwnCopy(t *testing.T) {
	t.Parallel()
	first := slices.Collect(annotation.DialectKeywords())
	want := slices.Clone(first)

	slices.Sort(first)
	slices.Reverse(first)
	first[0] = "mutated"
	first = append(first[:1], "appended")

	assert.Equal(t, want, slices.Collect(annotation.DialectKeywords()),
		"a second reader sees the list as the package declared it")
	assert.NotEqual(t, first, slices.Collect(annotation.DialectKeywords()))
}

// TestDialectKeyword_IsMembership holds the predicate a reader outside the
// package asks in place of searching the list: the dialect's keywords are
// members, and another keyword is not.
func TestDialectKeyword_IsMembership(t *testing.T) {
	t.Parallel()
	for keyword, want := range map[string]bool{
		"$id": true, "$schema": true, "$vocabulary": true,
		"$comment": false, "id": false, "": false,
	} {
		assert.Equal(t, want, annotation.DialectKeyword(keyword), "%q", keyword)
	}
}

// TestDecidedKeyword_IsMembership holds the predicate that replaced the
// exported list: the keywords another reader decides are members, and the
// dialect's own and an unknown keyword are not.
func TestDecidedKeyword_IsMembership(t *testing.T) {
	t.Parallel()
	for keyword, want := range map[string]bool{
		"$comment":       true,
		"$dynamicAnchor": true,
		"$dynamicRef":    true,
		"$id":            false,
		"$schema":        false,
		"bogus":          false,
		"":               false,
	} {
		assert.Equal(t, want, annotation.DecidedKeyword(keyword), "%q", keyword)
	}
	assert.ElementsMatch(t, []string{"$comment", "$dynamicAnchor", "$dynamicRef"}, annotation.DecidedKeywordList())
}
