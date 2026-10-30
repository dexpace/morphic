// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package detect_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/detect"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
)

// TestSniff_ReadsALargeDocumentWhole pins the reading a large document gets:
// the parse a small one gets, which finds a key wherever it sits and in either
// style, because where a writer put a key in a mapping says nothing about what
// the document is. Each case says whether the document parsed, so a case that
// expects no declaration cannot pass on bytes that never parsed at all.
func TestSniff_ReadsALargeDocumentWhole(t *testing.T) {
	t.Parallel()
	const filler = "# a line of padding that says nothing about the format\n"
	pad := strings.Repeat("p", scanCutover)
	cases := []struct {
		name, src  string
		want       detect.SniffProbe
		unreadable bool
	}{
		{"block yaml declaring first",
			padTo("openapi: 3.1.0\n", filler), detect.SniffProbe{OpenAPI: "3.1.0"}, false},
		{"block yaml declaring last",
			padTo("", filler) + "openapi: 3.1.0\n", detect.SniffProbe{OpenAPI: "3.1.0"}, false},
		{"flow json declaring first",
			`{"openapi":"3.1.0","x":"` + pad + `"}`, detect.SniffProbe{OpenAPI: "3.1.0"}, false},
		{"flow json declaring last",
			`{"x":"` + pad + `","openapi":"3.1.0"}`, detect.SniffProbe{OpenAPI: "3.1.0"}, false},
		{"flow json swagger first",
			`{"swagger":"2.0","x":"` + pad + `"}`, detect.SniffProbe{Swagger: "2.0"}, false},
		{"flow json swagger last",
			`{"x":"` + pad + `","swagger":"2.0"}`, detect.SniffProbe{Swagger: "2.0"}, false},
		// Neither YAML nor JSON: the parse fails and declares nothing.
		{"protobuf, large",
			padTo("syntax = \"proto3\";\n", "message M { string a = 1; }\n"), detect.SniffProbe{}, true},
		// The word is there and is not a key: a value is not a declaration.
		{"the word as a value, large",
			`{"x":"` + pad + `","note":"openapi"}`, detect.SniffProbe{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Greater(t, len(tc.src), scanCutover, "the case must be large to test it")
			probe, parsed, err := detect.Sniff(t.Context(), []byte(tc.src))
			assert.Equal(t, tc.want, probe)
			if tc.unreadable {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, parsed, "a document that parsed carries its parse")
		})
	}
}

// TestSniff_ReadsAMergeChainAsFarAsTheViewExpandsIt pins where the bound on the
// root's merges lies, nodeview's, and what a read it cuts short answers. A key
// past the bound may outrank what the read found, so the answer is unknown, and
// Recognize says the bound stopped it, unless the root's own pairs, which
// outrank every merge, settle it.
func TestSniff_ReadsAMergeChainAsFarAsTheViewExpandsIt(t *testing.T) {
	t.Parallel()
	const limit = nodeview.MergeDepthLimit
	// A version merged in beside the chain, listed after it, so the chain's
	// keys outrank it: the read finds it, and what the bound hid may still win.
	beside := strings.Replace(string(openapitest.RootMergeChain(limit+1, "openapi: 3.1.0", "")),
		fmt.Sprintf("<<: *a%d\n", limit), fmt.Sprintf("s: &s {openapi: 3.0.3}\n<<: [*a%d, *s]\n", limit), 1)
	require.Contains(t, beside, "<<: [*a", "the root merges the chain and then s")
	cases := map[string]struct {
		src     []byte
		version string
		cut     bool
	}{
		"a chain the view expands": {openapitest.RootMergeChain(limit, "openapi: 3.0.3", ""), "3.0.3", false},
		"a chain past it":          {openapitest.RootMergeChain(limit+1, "openapi: 3.0.3", ""), "", true},
		"a version the root writes itself": {
			openapitest.RootMergeChain(limit+1, "openapi: 3.0.3", "openapi: 3.1.0\n"), "3.1.0", false},
		"a merged version a deeper one may outrank": {[]byte(beside), "", true},
		"a root writing no version and a swagger one": {
			openapitest.RootMergeChain(limit+1, "openapi: 3.0.3", "openapi: prose words\nswagger: '2.0'\n"), "", false},
		"a root writing no version and leaving swagger to its merges": {
			openapitest.RootMergeChain(limit+1, "swagger: '2.0'", "openapi: prose words\n"), "", true},
		"a root writing swagger and leaving openapi to its merges": {
			openapitest.RootMergeChain(limit+1, "x: 1", "swagger: '2.0'\n"), "", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			probe, _, err := detect.Sniff(t.Context(), tc.src)
			require.NoError(t, err, "a read cut short is no parse failure")
			assert.Equal(t, tc.cut, probe.Cut)
			if tc.cut {
				return
			}
			assert.Equal(t, tc.version, probe.OpenAPI)
		})
	}
}

// TestDecodeYAML_RefusesARootThatIsNoMapping pins the shape complaint. A source
// with no root mapping has no top-level keys, and saying so is what lets
// Recognize report bytes that declare a key it serves and will not read.
func TestDecodeYAML_RefusesARootThatIsNoMapping(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, wantErr string }{
		{"sequence", "- openapi: 3.1.0\n", "!!seq"},
		{"scalar", "just a string\n", "!!str"},
		{"empty", "", ""},
		{"comment only", "# openapi: 3.1.0\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe, _, err := detect.DecodeYAML(t.Context(), []byte(tc.src))
			assert.Empty(t, probe.OpenAPI)
			if tc.wantErr == "" {
				assert.NoError(t, err, "bytes that carry no document decline rather than fail")
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr, "the complaint names the shape that was read")
		})
	}
}

// TestSniff_CostIsNotQuadraticInRepeatedKeys guards the half of the defect an
// answer cannot see. Reading two keys off a parsed tree is linear; decoding the
// root mapping to read them is quadratic in how often a key repeats, since
// yaml.v3 compares every pair of keys before reading any.
//
// Allocation count is the probe because it is deterministic where wall time is
// not. Doubling the repeats doubles the parse, so the bound sits between that
// and a quadratic term: at this size the linear reading grows by 1.97 and the
// quadratic one by 4.64.
func TestSniff_CostIsNotQuadraticInRepeatedKeys(t *testing.T) {
	head := "openapi: 3.0.3\ninfo: {}\n"
	small := []byte(head + strings.Repeat("x: y\n", dupRepeats))
	large := []byte(head + strings.Repeat("x: y\n", dupRepeats*2))

	smallAllocs := testing.AllocsPerRun(2, func() { _, _, _ = detect.Sniff(t.Context(), small) })
	largeAllocs := testing.AllocsPerRun(2, func() { _, _, _ = detect.Sniff(t.Context(), large) })

	require.Positive(t, smallAllocs, "a measurement of nothing bounds nothing")
	assert.Less(t, largeAllocs, smallAllocs*3,
		"twice the repeats must cost about twice, not about four times")
}

// TestDecodeYAML_RefusesAVersionKeyThatIsNoScalar pins the complaint for a
// version key whose value is a mapping or a sequence, written directly and
// reached through a `<<`. Such bytes name a key this compiler serves and do not
// say what dialect, so the probe cannot read them.
//
// Whether Recognize reports that or declines in silence is declaresProbeKey's
// answer and not this one's, and
// TestRecognize_AVersionKeyOfTheWrongShapeIsNoDeclaration asserts it: a
// mapping under the key is another tool's configuration as often as a broken
// spec, and declares no version either way, so Recognize declines all of them.
func TestDecodeYAML_RefusesAVersionKeyThatIsNoScalar(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, wantErr string }{
		{"mapping", "openapi: {a: b}\ninfo: {}\n", "!!map"},
		{"sequence", "openapi: [3.1.0]\ninfo: {}\n", "!!seq"},
		{"merged mapping", "base: &b\n  openapi: {a: b}\n<<: *b\n", "!!map"},
		{"merged through a sequence", "base: &b\n  openapi: {a: b}\n<<: [*b]\n", "!!map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe, _, err := detect.DecodeYAML(t.Context(), []byte(tc.src))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr, "the complaint names the shape that was read")
			assert.Empty(t, probe.OpenAPI)
		})
	}
}

// TestDecodeYAML_PassesOverWhatNamesNoVersion holds the walk to reading only
// what it came for. A mapping may key an entry with a sequence, and a `<<` may
// be written with a value that merges nothing; both are the source's business
// and neither stops the two keys beside them from being read.
func TestDecodeYAML_PassesOverWhatNamesNoVersion(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src string }{
		{"a key that is a sequence", "? [a, b]\n: v\nopenapi: 3.1.0\n"},
		{"a merge of a scalar", "<<: not-a-mapping\nopenapi: 3.1.0\n"},
		{"a merge of a sequence of scalars", "<<: [x, y]\nopenapi: 3.1.0\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe, _, err := detect.DecodeYAML(t.Context(), []byte(tc.src))
			require.NoError(t, err)
			assert.Equal(t, "3.1.0", probe.OpenAPI)
		})
	}
}

// TestProbeVersion_RefusesAnAliasThatResolvedToNothing covers the guard on a
// value nodeview could not dereference. A parser resolves every alias it
// accepts, so the node is built here rather than parsed: the guard exists
// because reading the value is what the next line does, and a nil there is a
// panic in detection, which runs before the compiler has decided the bytes are
// even its own.
func TestProbeVersion_RefusesAnAliasThatResolvedToNothing(t *testing.T) {
	t.Parallel()
	root := ynode.Map(ynode.Scalar("openapi"), &yaml.Node{Kind: yaml.AliasNode})
	_, err := detect.ProbeFromMapping(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no value")
}

// breadthMergeDoc writes a document whose root merges one anchor k times, where
// that anchor's mapping merges another k times. It is the shape that made the
// probe's walk quadratic: the inner mapping was entered once per merge key
// naming it, so the work was the product of the two rather than their sum. The
// version sits at the innermost anchor, so a walk that stops early is caught by
// the assertion rather than looking cheap.
func breadthMergeDoc(k int) []byte {
	var b strings.Builder
	b.WriteString("l0: &a0\n  openapi: 3.1.0\n")
	b.WriteString("l1: &a1\n")
	for range k {
		b.WriteString("  <<: *a0\n")
	}
	for range k {
		b.WriteString("<<: *a1\n")
	}
	return []byte(b.String())
}

// TestProbeFromMapping_CostIsLinearInMergeBreadth pins the bound GitHub #487
// was about: the merge bound limits how deep a chain is followed, not how wide,
// and a walk entering an anchored mapping once per merge key naming it costs
// the product of the two. nodeview's memo is what keeps it a sum. It counts
// allocations, not time, so load cannot move it, and runs serially because
// AllocsPerRun measures the whole process.
func TestProbeFromMapping_CostIsLinearInMergeBreadth(t *testing.T) {
	for _, k := range []int{2000, 4000} {
		var root yaml.Node
		require.NoError(t, yaml.Unmarshal(breadthMergeDoc(k), &root))
		content := detect.DocumentRoot(&root)

		var probe detect.SniffProbe
		var err error
		allocs := testing.AllocsPerRun(1, func() {
			probe, err = detect.ProbeFromMapping(content)
		})
		require.NoError(t, err)
		require.Equal(t, "3.1.0", probe.OpenAPI,
			"the version sits at the innermost anchor, so a walk that stops early fails here rather than looking cheap")
		assert.Less(t, allocs, float64(k),
			"a walk of %d merge keys made %.0f allocations, not fewer than the keys", k, allocs)
	}
}
