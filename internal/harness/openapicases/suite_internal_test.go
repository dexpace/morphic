// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompatible_TheReadmesExamples holds compatible to the seven examples of
// the suite's README, at a release on each side of every bound they name.
func TestCompatible_TheReadmesExamples(t *testing.T) {
	releases := []int{3, 4, 6, 7, 2019, 2020, 9999}
	for _, tc := range []struct {
		expr    string
		applies []int
	}{
		{"7", []int{7, 2019, 2020, 9999}},
		{"<=2019", []int{3, 4, 6, 7, 2019}},
		{"6,<=2019", []int{6, 7, 2019}},
		{"=2020", []int{2020}},
		{"=3,7,<=2020", []int{3, 7, 2019, 2020}},
		{"<=3,7", []int{3, 7, 2019, 2020, 9999}},
		{"9999", []int{9999}},
		{"", releases},
	} {
		var got []int
		for _, r := range releases {
			applies, err := compatible(tc.expr, r)
			require.NoError(t, err, tc.expr)
			if applies {
				got = append(got, r)
			}
		}
		assert.Equal(t, tc.applies, got, "%q", tc.expr)
	}
}

func TestCompatible_RefusesATermNamingNoRelease(t *testing.T) {
	for _, expr := range []string{"x", "<=", "=a", "-3", "6,,7", "<=-1"} {
		_, err := compatible(expr, release2020)
		require.Error(t, err, "%q", expr)
		assert.Contains(t, err.Error(), "a term naming no release", "%q", expr)
	}
}

// suiteFS holds the files of a fake suite under dir.
func suiteFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys["tests/"+name] = &fstest.MapFile{Data: []byte(data)}
	}
	return fsys
}

func TestReadGroups_NamesTheGroupsApplyingAtTheRelease(t *testing.T) {
	fsys := suiteFS(map[string]string{
		"b.json": `{"description": "b", "tests": [
			{"description": "d", "schema": true, "tests": []},
			{"description": "old", "compatibility": "<=7", "schema": {}, "tests": []},
			{"description": "d", "compatibility": "2019", "schema": {"type": "string"},
			 "externalSchemas": {"http://localhost:1234/x.json": {"type": "integer"}},
			 "tests": [{"description": "a string", "data": "s", "valid": true},
			           {"description": "a number", "data": 1, "valid": false}]}]}`,
		"a.json": `{"$schema": "s", "description": "a", "tests": [
			{"description": "x", "specification": [], "comment": "c", "tags": ["t"], "schema": {}, "tests": []}]}`,
	})
	groups, err := readGroups(fsys, "tests", release2020)
	require.NoError(t, err)
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
	}
	assert.Equal(t, []string{"a.json/x", "b.json/d", "b.json/d#2"}, names)

	last := groups[2]
	assert.JSONEq(t, `{"type": "string"}`, string(last.Schema))
	assert.JSONEq(t, `{"type": "integer"}`, string(last.External["http://localhost:1234/x.json"]))
	require.Len(t, last.Tests, 2)
	assert.Equal(t, "a string", last.Tests[0].Description)
	assert.JSONEq(t, `"s"`, string(last.Tests[0].Data))
	assert.True(t, last.Tests[0].Valid)
	assert.False(t, last.Tests[1].Valid)
}

func TestReadGroups_RefusesWhatTheSuiteDoesNotWrite(t *testing.T) {
	group := func(g string) string { return `{"description": "f", "tests": [` + g + `]}` }
	for _, tc := range []struct {
		name string
		fsys fstest.MapFS
		want string
	}{
		{"no directory", fstest.MapFS{}, "open tests: file does not exist"},
		{"a directory among the files", fstest.MapFS{"tests/d.json/x": {}}, "d.json"},
		{"a file that is no JSON", suiteFS(map[string]string{"f.json": `{`}), "f.json"},
		{"a member the suite does not write", suiteFS(map[string]string{"f.json": group(
			`{"description": "g", "schema": {}, "tests": [], "todo": 1}`)}), `unknown object member name "todo"`},
		{"a compatibility that does not parse", suiteFS(map[string]string{"f.json": group(
			`{"description": "g", "compatibility": "7,x", "schema": {}, "tests": []}`)}), "group 0: compatibility"},
		{"a group writing no schema", suiteFS(map[string]string{"f.json": group(
			`{"description": "g", "tests": []}`)}), `group 0: "g" writes no schema`},
		{"a description holding a tab", suiteFS(map[string]string{"f.json": group(
			`{"description": "g\th", "schema": {}, "tests": []}`)}), "holds a tab or a line break"},
		{"an instance not saying whether it is valid", suiteFS(map[string]string{"f.json": group(
			`{"description": "g", "schema": {}, "tests": [{"description": "i", "data": 1}]}`)}),
			"test 0 says nothing of whether it is valid"},
		{"no group at the release", suiteFS(map[string]string{"f.json": group(
			`{"description": "g", "compatibility": "<=7", "schema": {}, "tests": []}`)}),
			"no group in tests applies at release 2020"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readGroups(tc.fsys, "tests", release2020)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "openapicases: the test suite: ")
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
