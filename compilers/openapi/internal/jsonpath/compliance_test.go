// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/jsonpath"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// suitePath is the vendored RFC 9535 compliance suite.
const suitePath = "testdata/jsonpath-compliance-test-suite/cts.json"

// Counts of the suite at the revision its README names: every case, and the
// ones that are invalid selectors. A refresh moves them with the file.
const (
	suiteCases   = 715
	suiteInvalid = 249
)

// skipped holds the cases the evaluator is excused from, each with its reason.
// It is empty: the evaluator passes the whole suite. An entry naming no case,
// or a case that passes, fails TestCompliance, so a skip cannot outlive its
// reason.
var skipped = map[string]string{}

// ctsCase is one case of the suite: a selector, and either that it is invalid
// or the document with what the query returns. A case with several results
// lists each as results, any of which may be returned (a nodelist whose order
// the RFC leaves open).
type ctsCase struct {
	Name, Selector string
	Document       jsontext.Value
	Result         []jsontext.Value
	ResultPaths    []string
	Results        [][]jsontext.Value
	ResultsPaths   [][]string
	Invalid        bool
}

// readCase decodes one case from its members, named as the suite names them:
// the suite's snake_case names are the file's, not this package's.
func readCase(t testing.TB, members map[string]jsontext.Value) ctsCase {
	t.Helper()
	var ctsCaseValue ctsCase
	for name, into := range map[string]any{
		"name": &ctsCaseValue.Name, "selector": &ctsCaseValue.Selector, "result": &ctsCaseValue.Result, "result_paths": &ctsCaseValue.ResultPaths,
		"results": &ctsCaseValue.Results, "results_paths": &ctsCaseValue.ResultsPaths, "invalid_selector": &ctsCaseValue.Invalid,
	} {
		if raw, ok := members[name]; ok {
			require.NoError(t, json.Unmarshal(raw, into), "the member %q of a case", name)
		}
	}
	ctsCaseValue.Document = members["document"]
	return ctsCaseValue
}

// alternatives returns each acceptable answer of a valid case: its values and
// the normalized paths that go with them.
func (c ctsCase) alternatives() (values [][]jsontext.Value, paths [][]string) {
	if c.Results != nil {
		return c.Results, c.ResultsPaths
	}
	return [][]jsontext.Value{c.Result}, [][]string{c.ResultPaths}
}

// TestCompliance holds the evaluator to every case of the RFC 9535 compliance
// suite: an invalid selector is a *SyntaxError, and a valid one returns the
// nodes and the normalized paths the case lists. A case neither passing nor in
// skipped fails, and so does a skip whose case passes.
func TestCompliance(t *testing.T) {
	data, err := os.ReadFile(suitePath)
	require.NoError(t, err)
	var file struct {
		Tests []map[string]jsontext.Value `json:"tests"`
	}
	require.NoError(t, json.Unmarshal(data, &file))
	require.Len(t, file.Tests, suiteCases, "the suite at the revision its README names")
	var suite struct{ Tests []ctsCase }
	for _, members := range file.Tests {
		suite.Tests = append(suite.Tests, readCase(t, members))
	}

	names := make(map[string]bool, len(suite.Tests))
	invalid, run := 0, 0
	for _, tc := range suite.Tests {
		require.False(t, names[tc.Name], "the suite names %q twice, so a skip could not name one case", tc.Name)
		names[tc.Name] = true
		if tc.Invalid {
			invalid++
		}
	}
	require.Equal(t, suiteInvalid, invalid)
	for name := range skipped {
		require.True(t, names[name], "the skip table names %q, which is no case", name)
	}

	for _, tc := range suite.Tests {
		t.Run(tc.Name, func(t *testing.T) {
			run++
			failure := checkCase(t, tc)
			reason, skip := skipped[tc.Name]
			switch {
			case skip && failure == nil:
				t.Fatalf("%q is skipped (%s) and passes: remove the skip", tc.Name, reason)
			case skip:
				t.Skipf("skipped: %s", reason)
			case failure != nil:
				t.Fatal(failure)
			}
		})
	}
	assert.Equal(t, len(suite.Tests), run, "every case ran")
}

// checkCase evaluates one case and returns how it differs from what the suite
// lists, nil when it agrees. It is an error, not a failure of t, so a skipped
// case can be told to fail and a skip that no longer does can be reported.
func checkCase(t *testing.T, tc ctsCase) error {
	t.Helper()
	query, err := jsonpath.Parse(tc.Selector)
	if tc.Invalid {
		if err == nil {
			return fmt.Errorf("the selector %q is no RFC 9535 query, and Parse accepted it", tc.Selector)
		}
		if _, ok := errors.AsType[*jsonpath.SyntaxError](err); !ok {
			return fmt.Errorf("the selector %q is no RFC 9535 query, and Parse refused it as another error: %w", tc.Selector, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("the selector %q is valid, and Parse refused it: %w", tc.Selector, err)
	}

	root := jsonTree(t, tc.Document)
	b := budget.New(budget.Unbounded())
	got, err := query.Select(t.Context(), nodeview.New(), root, &b)
	if err != nil {
		return fmt.Errorf("selecting: %w", err)
	}
	gotValues := make([]string, len(got))
	gotPaths := make([]string, len(got))
	for i, located := range got {
		gotValues[i] = string(canonical(t, renderJSON(t, located.Node)))
		gotPaths[i] = located.Path.String()
	}

	values, paths := tc.alternatives()
	require.Len(t, paths, len(values), "the case lists as many paths as results")
	var diffs []string
	for i := range values {
		want := make([]string, len(values[i]))
		for j, v := range values[i] {
			want[j] = string(canonical(t, v))
		}
		if slices.Equal(want, gotValues) && slices.Equal(paths[i], gotPaths) {
			return nil
		}
		diffs = append(diffs, cmp.Diff(want, gotValues)+cmp.Diff(paths[i], gotPaths))
	}
	return fmt.Errorf("no listed answer matches (-want +got), one block per answer:\n%s", strings.Join(diffs, "\n--\n"))
}

// jsonTree builds the tree doc stands for as the compiler's reader builds one
// from a JSON text: mappings and sequences tagged !!map and !!seq, strings
// !!str, and numbers, booleans and null by what they are. A name written twice
// stays twice, for the evaluator to read as its last.
func jsonTree(t testing.TB, doc []byte) *yaml.Node {
	t.Helper()
	dec := jsontext.NewDecoder(bytes.NewReader(doc), jsontext.AllowDuplicateNames(true))
	var root *yaml.Node
	var stack []*yaml.Node
	for {
		tok, err := dec.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if kind := tok.Kind(); kind == '}' || kind == ']' {
			stack = stack[:len(stack)-1]
			continue
		}
		n := tokenNode(tok)
		if len(stack) == 0 {
			root = n
		} else {
			top := stack[len(stack)-1]
			top.Content = append(top.Content, n)
		}
		if n.Kind != yaml.ScalarNode {
			stack = append(stack, n)
		}
	}
	require.NotNil(t, root, "the document holds a value")
	return root
}

// tokenNode returns the node an opening or a scalar token stands for.
func tokenNode(tok jsontext.Token) *yaml.Node {
	switch tok.Kind() {
	case '{':
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	case '[':
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	case '"':
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: tok.String()}
	case 'n':
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	case 't', 'f':
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: tok.String()}
	default:
		tag := "!!int"
		if strings.ContainsAny(tok.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: tok.String()}
	}
}

// renderJSON writes the tree under n as JSON, by its tags alone: the second
// reading of a tree the suite's comparison rests on, which shares nothing with
// the evaluator's own. A mapping's members are written as the tree holds them.
func renderJSON(t testing.TB, n *yaml.Node) jsontext.Value {
	t.Helper()
	var out bytes.Buffer
	writeJSON(t, &out, n)
	return jsontext.Value(out.Bytes())
}

// writeJSON appends the JSON of the tree under n to out.
func writeJSON(t testing.TB, out *bytes.Buffer, n *yaml.Node) {
	t.Helper()
	switch n.Kind {
	case yaml.MappingNode:
		out.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				out.WriteByte(',')
			}
			writeJSON(t, out, n.Content[i])
			out.WriteByte(':')
			writeJSON(t, out, n.Content[i+1])
		}
		out.WriteByte('}')
	case yaml.SequenceNode:
		out.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				out.WriteByte(',')
			}
			writeJSON(t, out, c)
		}
		out.WriteByte(']')
	case yaml.ScalarNode:
		if n.Tag == "!!str" {
			enc, err := json.Marshal(n.Value)
			require.NoError(t, err)
			out.Write(enc)
			return
		}
		out.WriteString(n.Value)
	default:
		t.Fatalf("a result node of kind %d: the evaluator returns values, never aliases or documents", n.Kind)
	}
}

// canonical returns v in RFC 8785's form, so two spellings of one value compare
// equal as text: members in sorted order, numbers in their shortest form.
func canonical(t testing.TB, v jsontext.Value) jsontext.Value {
	t.Helper()
	c := slices.Clone(v)
	require.NoError(t, c.Canonicalize(), "canonicalizing %s", strconv.Quote(string(v)))
	return c
}
