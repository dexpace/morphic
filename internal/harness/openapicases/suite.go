// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// suiteDir holds the vendored JSON Schema Test Suite's test files
// (testdata/json-schema-test-suite/README.md).
const suiteDir = "testdata/json-schema-test-suite/validation/tests"

//go:embed testdata/json-schema-test-suite/validation/tests/*.json
var suiteFiles embed.FS

// release2020 is JSON Schema 2020-12 as the suite's compatibility expressions
// number it.
const release2020 = 2020

// Group is one test case of the vendored JSON Schema Test Suite: a schema, the
// schemas its references name, and the instances it is held to.
type Group struct {
	// Name is "<file>/<description>", "#<n>" after the nth occurrence of a
	// description its file repeats.
	Name string
	// Schema is the group's schema.
	Schema jsontext.Value
	// External holds the schemas the group serves at their retrieval URIs
	// (externalSchemas).
	External map[string]jsontext.Value
	// Tests are the instances, in the suite's order.
	Tests []Test
}

// Test is one instance of a group, and whether the suite holds it valid.
type Test struct {
	Description string
	Data        jsontext.Value
	Valid       bool
}

// suiteFile is one test file as the suite writes it. Every member a file may
// write at the vendored revision is named, so a refresh writing another fails
// to read until it is read deliberately.
type suiteFile struct {
	Dialect     string       `json:"$schema"`
	Description string       `json:"description"`
	Tests       []suiteGroup `json:"tests"`
}

// suiteGroup is one group as the suite writes it.
type suiteGroup struct {
	Description   string                    `json:"description"`
	Compatibility string                    `json:"compatibility"`
	Schema        jsontext.Value            `json:"schema"`
	External      map[string]jsontext.Value `json:"externalSchemas"`
	Tests         []suiteTest               `json:"tests"`
	// Specification, Comment and Tags are read and not used: no reading here
	// turns on them (README.md, "Selection").
	Specification jsontext.Value `json:"specification"`
	Comment       string         `json:"comment"`
	Tags          []string       `json:"tags"`
}

// suiteTest is one instance as the suite writes it.
type suiteTest struct {
	Description string         `json:"description"`
	Data        jsontext.Value `json:"data"`
	Valid       *bool          `json:"valid"`
	Comment     string         `json:"comment"`
}

// SuiteGroups returns the vendored suite's groups that apply at JSON Schema
// 2020-12, file by file in name order and in each file's order.
func SuiteGroups() ([]Group, error) { return readGroups(suiteFiles, suiteDir, release2020) }

// readGroups returns the groups applying at release of each test file in dir
// of fsys. A file that does not read as the suite's format, a group writing no
// schema or a compatibility expression that does not parse, and an instance
// not saying whether it is valid are errors.
func readGroups(fsys fs.FS, dir string, release int) ([]Group, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("openapicases: the test suite: %w", err)
	}
	var groups []Group
	for _, e := range entries {
		data, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("openapicases: the test suite: %w", err)
		}
		read, err := fileGroups(e.Name(), data, release)
		if err != nil {
			return nil, fmt.Errorf("openapicases: the test suite: %s: %w", e.Name(), err)
		}
		groups = append(groups, read...)
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("openapicases: the test suite: no group in %s applies at release %d", dir, release)
	}
	return groups, nil
}

// fileGroups returns the groups of the test file name, holding data, that
// apply at release, naming each by its description: the nth occurrence of a
// repeated one, counted over the whole file, takes "#<n>" after it.
func fileGroups(name string, data []byte, release int) ([]Group, error) {
	var file suiteFile
	if err := json.Unmarshal(data, &file, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	var groups []Group
	seen := map[string]int{}
	for i, suiteGroup := range file.Tests {
		seen[suiteGroup.Description]++
		applies, err := compatible(suiteGroup.Compatibility, release)
		if err != nil {
			return nil, fmt.Errorf("group %d: %w", i, err)
		}
		if !applies {
			continue
		}
		group, err := suiteGroup.group(name, seen[suiteGroup.Description])
		if err != nil {
			return nil, fmt.Errorf("group %d: %w", i, err)
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// group returns g, the nth group of the file name writing its description.
func (g suiteGroup) group(name string, n int) (Group, error) {
	if len(g.Schema) == 0 {
		return Group{}, fmt.Errorf("%q writes no schema", g.Description)
	}
	if strings.ContainsAny(g.Description, "\t\n\r") {
		return Group{}, fmt.Errorf("%q holds a tab or a line break, which no record's line holds", g.Description)
	}
	out := Group{Name: name + "/" + g.Description, Schema: g.Schema, External: g.External}
	if n > 1 {
		out.Name += "#" + strconv.Itoa(n)
	}
	for j, suiteTest := range g.Tests {
		if suiteTest.Valid == nil {
			return Group{}, fmt.Errorf("%q: test %d says nothing of whether it is valid", g.Description, j)
		}
		out.Tests = append(out.Tests, Test{Description: suiteTest.Description, Data: suiteTest.Data, Valid: *suiteTest.Valid})
	}
	return out, nil
}

// compatible reports whether a group whose compatibility expression is expr
// applies at release, by the algorithm the suite's README gives: the
// comma-separated terms are read in order, each deciding alone, "N" for a
// release at or past N, "<=N" for one at or before it and "=N" for N alone, and
// the first term whose N is at or past release is the last read. Every term is
// parsed, read or not. An empty expression applies at every release.
func compatible(expr string, release int) (bool, error) {
	if expr == "" {
		return true, nil
	}
	applies, decided := true, false
	for term := range strings.SplitSeq(expr, ",") {
		op, n, err := compatibilityTerm(strings.TrimSpace(term))
		if err != nil {
			return false, fmt.Errorf("compatibility %q: %w", expr, err)
		}
		if decided {
			continue
		}
		switch op {
		case "<=":
			applies = release <= n
		case "=":
			applies = release == n
		default:
			applies = release >= n
		}
		decided = release <= n
	}
	return applies, nil
}

// compatibilityTerm splits one term of a compatibility expression into its
// operator, "<=", "=" or none, and its release.
func compatibilityTerm(term string) (string, int, error) {
	op := ""
	for _, o := range []string{"<=", "="} {
		if rest, ok := strings.CutPrefix(term, o); ok {
			op, term = o, rest
			break
		}
	}
	n, err := strconv.Atoi(term)
	if err != nil || n < 0 {
		return "", 0, fmt.Errorf("a term naming no release: %q", op+term)
	}
	return op, n, nil
}
