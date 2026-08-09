package openapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

func TestDetect_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, path, src string
		want            compilers.SourceFormat
		wantOK          bool
	}{
		{"openapi 3.1 yaml", "api.yaml", "openapi: 3.1.0\ninfo: {}\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true},
		{"openapi 3.0 json", "api.json", `{"openapi": "3.0.3"}`,
			compilers.SourceFormat{Name: "openapi", Version: "3.0"}, true},
		{"openapi 3.2 yaml", "api.yaml", "openapi: 3.2.0\ninfo: {}\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.2"}, true},
		// A version already in major.minor form (single dot) exercises
		// majorMinor's unchanged-passthrough return.
		{"openapi major.minor only", "api.yaml", "openapi: \"3.1\"\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true},
		// A bare-major version (no dot) also reaches the passthrough return.
		{"openapi bare major", "api.yaml", "openapi: \"4\"\n",
			compilers.SourceFormat{Name: "openapi", Version: "4"}, true},
		// Recognized, and deliberately not a format Formats reports: naming it
		// lets the caller say "unsupported" rather than "unreadable".
		{"swagger", "api.yaml", "swagger: \"2.0\"\n",
			compilers.SourceFormat{Name: "swagger", Version: "2.0"}, true},

		// Everything below is another format's input, and none of it is an error
		// here: three of the five planned compilers take bytes that are not YAML.
		{"protobuf", "svc.proto", "syntax = \"proto3\";\nservice S { rpc Get (Q) returns (A); }\n",
			compilers.SourceFormat{}, false},
		{"typespec", "main.tsp", "import \"@typespec/http\";\nmodel Pet { name: string; }\n",
			compilers.SourceFormat{}, false},
		{"graphql", "s.graphql", "type Query {\n  pet(id: ID!): Pet\n}\n",
			compilers.SourceFormat{}, false},
		// Valid YAML that declares neither key: whether a source parses is not
		// the question detection answers.
		{"graphql that parses as yaml", "s.graphql", "type Query { a: String }\n",
			compilers.SourceFormat{}, false},
		{"yaml that is no spec", "junk.yaml", "hello: world\n",
			compilers.SourceFormat{}, false},
		{"unparseable yaml", "api.yaml", "openapi: [unterminated\n",
			compilers.SourceFormat{}, false},
		{"empty", "empty.yaml", "", compilers.SourceFormat{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := New().Detect(compilers.Source{Path: tc.path, Data: []byte(tc.src)})
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// padTo returns src grown past the sniff cap by appending filler, so sniff takes
// its bounded-prefix path rather than decoding the source whole.
func padTo(src, filler string) string {
	var b strings.Builder
	b.WriteString(src)
	for b.Len() <= maxSniffBytes {
		b.WriteString(filler)
	}
	return b.String()
}

// TestSniff_BeyondTheCap pins what the bound buys and what it costs. A document
// larger than the cap is read from its first maxSniffBytes in whichever style it
// is written, and a declaration past that point is not seen — detection stays
// flat in document size rather than paying a full parse to read two keys.
func TestSniff_BeyondTheCap(t *testing.T) {
	t.Parallel()
	const filler = "# a line of padding that says nothing about the format\n"
	cases := []struct {
		name, src string
		want      sniffProbe
	}{
		{"block yaml declaring first",
			padTo("openapi: 3.1.0\n", filler), sniffProbe{OpenAPI: "3.1.0"}},
		{"block yaml declaring past the cap",
			padTo("", filler) + "openapi: 3.1.0\n", sniffProbe{}},
		{"flow json declaring first",
			`{"openapi":"3.1.0","x":"` + strings.Repeat("p", maxSniffBytes) + `"}`,
			sniffProbe{OpenAPI: "3.1.0"}},
		{"flow json declaring past the cap",
			`{"x":"` + strings.Repeat("p", maxSniffBytes) + `","openapi":"3.1.0"}`,
			sniffProbe{}},
		{"flow json swagger first",
			`{"swagger":"2.0","x":"` + strings.Repeat("p", maxSniffBytes) + `"}`,
			sniffProbe{Swagger: "2.0"}},
		// Neither YAML nor JSON, and larger than the cap: the prefix is parsed,
		// fails, and the answer is silence rather than a parser's complaint.
		{"protobuf past the cap",
			padTo("syntax = \"proto3\";\n", "message M { string a = 1; }\n"), sniffProbe{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Greater(t, len(tc.src), maxSniffBytes, "the case must exceed the cap to test it")
			assert.Equal(t, tc.want, sniff([]byte(tc.src)))
		})
	}
}

func TestDecodeFlowPrefix_ReadsWhatTheCutLeft(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, prefix string
		want         sniffProbe
		wantFlow     bool
	}{
		{"complete document", `{"openapi":"3.1.0","info":{"title":"T"}}`,
			sniffProbe{OpenAPI: "3.1.0"}, true},
		{"cut inside a later value", `{"openapi":"3.1.0","info":{"title":"T`,
			sniffProbe{OpenAPI: "3.1.0"}, true},
		{"cut inside a key", `{"openapi":"3.1.0","inf`,
			sniffProbe{OpenAPI: "3.1.0"}, true},
		{"swagger", `{"swagger":"2.0","info":{}}`, sniffProbe{Swagger: "2.0"}, true},
		// A version that is not a string declares no dialect, and must not be
		// read as one by accident.
		{"non-string version", `{"openapi":3}`, sniffProbe{}, true},
		{"no flow mapping", "openapi: 3.1.0\n", sniffProbe{}, false},
		{"not even a token", "\x00", sniffProbe{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, flow := decodeFlowPrefix([]byte(tc.prefix))
			assert.Equal(t, tc.wantFlow, flow)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDecodeFlowPrefix_StopsAtTheEntryCap proves the walk is bounded by its own
// count and not only by the byte cap: a declaration after maxSniffEntries other
// entries is not read.
func TestDecodeFlowPrefix_StopsAtTheEntryCap(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteByte('{')
	for i := range maxSniffEntries + 1 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"k`)
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(strings.Repeat("y", i%7))
		b.WriteString(`":0`)
	}
	b.WriteString(`,"openapi":"3.1.0"}`)

	got, flow := decodeFlowPrefix([]byte(b.String()))
	require.True(t, flow)
	assert.Equal(t, sniffProbe{}, got, "the entry past the cap is not read")
}

func TestWholeLines(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a\nb\n", string(wholeLines([]byte("a\nb\nc"))))
	assert.Equal(t, "nolines", string(wholeLines([]byte("nolines"))),
		"a prefix with no newline has no better cut to make")
}

func TestMajorMinor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "3.1", majorMinor("3.1.0"))
	assert.Equal(t, "3.1", majorMinor("3.1"))
	assert.Equal(t, "4", majorMinor("4"))
}
