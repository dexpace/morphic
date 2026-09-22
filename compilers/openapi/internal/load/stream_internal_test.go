package load

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// streamDiags returns the dropped-document diagnostics among diags.
func streamDiags(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == diag.StreamDocumentsDropped {
			out = append(out, d)
		}
	}
	return out
}

// TestLoad_AStreamLowersItsFirstDocumentAndReportsTheRest pins the fix for
// GitHub #387 on the fixture the harness sweeps. A YAML stream of two OpenAPI
// documents used to compile the first and drop the second with no diagnostic
// and exit 0. The first is still what is lowered — an OpenAPI document is one
// YAML document — but the drop is now an error naming where the dropped
// content begins.
func TestLoad_AStreamLowersItsFirstDocumentAndReportsTheRest(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../testdata/openapi/stream_two_documents.yaml")
	require.NoError(t, err)

	doc, diags, err := Load(t.Context(), 3, compilers.Source{Path: "stream.yaml", Data: data}, Options{})

	require.NoError(t, err, "a stream is a spec problem, not a Go error")
	require.NotNil(t, doc, "the first document is lowered")
	assert.Equal(t, "First", doc.Doc.Info.GetTitle())

	got := streamDiags(diags)
	require.Len(t, got, 1)
	assert.Equal(t, ir.SeverityError, got[0].Severity, "content the IR does not hold is a losslessness failure")
	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "7:1"}, got[0].Provenance,
		"anchored where the first dropped document begins")
	assert.Contains(t, got[0].Message, "the one after it holds", "the count of what was dropped")
}

// TestLoad_ATrailingEmptyDocumentIsNotADrop is the control that keeps the
// diagnostic honest: a document separator with nothing after it — which tools
// and editors emit — starts a document that holds nothing, and nothing was
// lost by not lowering it. Only a document with content counts.
func TestLoad_ATrailingEmptyDocumentIsNotADrop(t *testing.T) {
	t.Parallel()
	for name, tail := range map[string]string{
		"a bare separator":    "---\n",
		"two bare separators": "---\n---\n",
		"an explicit null":    "--- null\n",
		"a tilde":             "--- ~\n",
		"a document end":      "...\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31+tail), Options{})

			require.NoError(t, err)
			require.NotNil(t, doc)
			assert.Empty(t, streamDiags(diags), "nothing was dropped: %+v", diags)
			assert.False(t, diag.HasError(diags), "and the document is clean: %+v", diags)
		})
	}
}

// TestLoad_AStreamCountsWhatItDrops pins the count: every document past the
// first that holds content is one, an empty one between them is not, and past
// the counting bound the diagnostic says "at least" rather than a number it
// stopped short of.
func TestLoad_AStreamCountsWhatItDrops(t *testing.T) {
	t.Parallel()
	second := "---\nopenapi: 3.1.0\ninfo: {title: N, version: \"1\"}\npaths: {}\n"

	_, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31+second+"---\n"+second+second), Options{})
	require.NoError(t, err)
	got := streamDiags(diags)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "the 3 after it")

	many := minimal31 + strings.Repeat(second, maxStreamDocuments+1)
	_, diags, err = Load(t.Context(), 0, openapitest.SourceOf(many), Options{})
	require.NoError(t, err)
	got = streamDiags(diags)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "at least "+strconv.Itoa(maxStreamDocuments)+" after it",
		"past the bound the count is a floor, never a number the walk did not reach")
}

// TestLoad_AStreamThatStopsParsingIsStillReported pins the tail nobody used
// to see: bytes past the first document that are not YAML. yaml.v3 parses one
// document per call, so the old decode never read them; now they are reported
// as dropped, counted with whatever parsed before them, and never turned into
// a parse failure of a first document that parsed fine.
func TestLoad_AStreamThatStopsParsingIsStillReported(t *testing.T) {
	t.Parallel()
	second := "---\nopenapi: 3.1.0\ninfo: {title: N, version: \"1\"}\npaths: {}\n"

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(minimal31+"---\n: : :\n"), Options{})
	require.NoError(t, err, "the first document parsed; what follows it is a spec problem")
	require.NotNil(t, doc)
	got := streamDiags(diags)
	require.Len(t, got, 1)
	assert.Equal(t, ir.Provenance{Source: 0}, got[0].Provenance, "nothing parsed to anchor on")
	assert.Contains(t, got[0].Message, "could not be parsed")
	assert.NotContains(t, got[0].Message, "after it", "no document holding content was counted")

	_, diags, err = Load(t.Context(), 0, openapitest.SourceOf(minimal31+second+"---\n: : :\n"), Options{})
	require.NoError(t, err)
	got = streamDiags(diags)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "the one after it holds")
	assert.Contains(t, got[0].Message, "could not be parsed")

	// Not everything yaml.v3 stops at is malformed: it refuses a %YAML directive
	// as incompatible wherever it appears, so a well-formed later document that
	// opens with one is reported as unparsed rather than counted. The wording
	// says "could not be parsed" and not "is not YAML" for this reason.
	doc, diags, err = Load(t.Context(), 0, openapitest.SourceOf(minimal31+"...\n%YAML 1.2\n---\nb: 1\n"), Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	got = streamDiags(diags)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "could not be parsed")
	assert.NotContains(t, got[0].Message, "after it")
}

// TestLoad_ASingleDocumentReportsNoStream is the control for every case above:
// an ordinary document, with or without its leading separator, is not a stream.
func TestLoad_ASingleDocumentReportsNoStream(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"plain":                 minimal31,
		"with a leading marker": "---\n" + minimal31,
		"with an explicit end":  minimal31 + "...\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(src), Options{})
			require.NoError(t, err)
			require.NotNil(t, doc)
			assert.Empty(t, streamDiags(diags))
		})
	}
}

// TestLoad_AStreamIsReportedEvenWhenTheFirstDocumentIsRefused pins that the
// drop rides outside the refusal channel: a first document the compiler
// refuses to lower — here for its version — still reports the documents
// dropped after it, and the drop does not itself read as a refusal of a first
// document that would otherwise load.
func TestLoad_AStreamIsReportedEvenWhenTheFirstDocumentIsRefused(t *testing.T) {
	t.Parallel()
	const unsupported = "openapi: 2.0.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	second := "---\nopenapi: 3.1.0\ninfo: {title: N, version: \"1\"}\npaths: {}\n"

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(unsupported+second), Options{})

	require.NoError(t, err)
	assert.Nil(t, doc, "the first document is refused")
	assert.Equal(t, 1, countErrorsAt(diags, diag.UnsupportedVersion), "for its version: %+v", diags)
	assert.Len(t, streamDiags(diags), 1, "and the drop is still reported: %+v", diags)
}

// TestHoldsContent_ReadsOnlyAWellFormedDocument pins the predicate the tail
// count turns on. yaml.v3 wraps exactly one root in every document it parses,
// so the shapes that fail the guard are built rather than decoded — the same
// reason TestUnmarshal_RejectsADocumentNodeHoldingMoreThanOneRoot builds its
// node — and a document that holds nothing is the null its separator decodes
// to, whatever the spelling.
func TestHoldsContent_ReadsOnlyAWellFormedDocument(t *testing.T) {
	t.Parallel()
	wrap := func(root *yaml.Node) *yaml.Node {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	assert.True(t, holdsContent(wrap(mapping)))
	assert.True(t, holdsContent(wrap(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "text"})))
	assert.False(t, holdsContent(wrap(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"})), "an explicit null")
	assert.False(t, holdsContent(wrap(&yaml.Node{Kind: yaml.ScalarNode})),
		"an untagged empty scalar resolves to null, which is what a bare separator decodes to")
	assert.False(t, holdsContent(mapping), "not a document node")
	assert.False(t, holdsContent(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping, mapping}}),
		"a document node yaml.v3 never produces")
}

// TestDecode_TakesTheFirstDocumentHoldingContent pins the fix for GitHub #481:
// the document the compile lowers is the first in the stream that holds
// content, not the first the stream opens. A leading document that decodes to
// null — a bare separator, a comment, any spelling of null, a tag that
// resolves to one — is what a file assembled from fragments or a stripped
// template begins with, and is skipped as a trailing one already was.
func TestDecode_TakesTheFirstDocumentHoldingContent(t *testing.T) {
	t.Parallel()
	for name, lead := range map[string]string{
		"two bare separators":      "---\n---\n",
		"a comment-only document":  "---\n# nothing here\n---\n",
		"a comment on the marker":  "--- # nothing here\n---\n",
		"an explicit null":         "--- null\n---\n",
		"a tilde":                  "--- ~\n---\n",
		"an upper-case null":       "--- NULL\n---\n",
		"a tagged null":            "--- !!null\n---\n",
		"a directive then empties": "%YAML 1.1\n---\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, rest, err := decode([]byte(lead + minimal31))
			require.NoError(t, err)
			require.True(t, holdsContent(root), "the document taken is the one with content")
			assert.Equal(t, yaml.MappingNode, root.Content[0].Kind)
			assert.Equal(t, strings.Count(lead, "\n")+1, root.Content[0].Line, "taken from where it was written")
			assert.Equal(t, tail{}, rest, "and nothing after it was dropped")
		})
	}
}

// TestDecode_AStreamOfEmptyDocumentsIsItsFirst pins what is taken when nothing
// holds content: the first document the stream opens, a null, which the model
// build then refuses exactly as it did before leading empties were skipped —
// not a node of no kind, which is the answer for a source with no document at
// all and reaches a different refusal.
func TestDecode_AStreamOfEmptyDocumentsIsItsFirst(t *testing.T) {
	t.Parallel()
	root, rest, err := decode([]byte("---\n---\n"))
	require.NoError(t, err)
	require.Equal(t, yaml.DocumentNode, root.Kind)
	assert.False(t, holdsContent(root))
	assert.Equal(t, 2, root.Content[0].Line, "the first of them, where the old decode stopped")
	assert.Equal(t, tail{}, rest, "the empties after it drop nothing")

	empty, _, err := decode([]byte(" "))
	require.NoError(t, err)
	assert.Equal(t, yaml.Kind(0), empty.Kind, "no document at all is still the node of no kind")
}

// TestDecode_AnErrorBeforeContentIsAParseError pins the one shape the search
// for content can meet that a first-document read never did: yaml.v3 refuses a
// document that follows an explicit end marker without its own `---`, and when
// nothing readable came before it, the source is unreadable.
func TestDecode_AnErrorBeforeContentIsAParseError(t *testing.T) {
	t.Parallel()
	_, _, err := decode([]byte("---\n...\n" + minimal31))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrParse)
}

// TestFirstDocument_IsWhatDecodeTakes pins the reader detection shares with
// the compile: the same document, so a source routed here is the source
// lowered here, and the parser's own error text, which detection quotes.
func TestFirstDocument_IsWhatDecodeTakes(t *testing.T) {
	t.Parallel()
	src := []byte("--- null\n---\n" + minimal31)
	fromDecode, _, err := decode(src)
	require.NoError(t, err)
	fromDetect, err := FirstDocument(src)
	require.NoError(t, err)
	assert.Equal(t, fromDecode.Content[0].Line, fromDetect.Content[0].Line)
	assert.Equal(t, yaml.MappingNode, fromDetect.Content[0].Kind)

	_, err = FirstDocument([]byte("\tnot: yaml\n"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrParse, "the parser's own error, unwrapped, for detection to quote")
}

// TestLoad_LeadingEmptyDocumentsAreSkipped is the end-to-end pin: the stream
// #481 reports loads clean, the tail count starts after the document taken,
// and the document is the one the author wrote.
func TestLoad_LeadingEmptyDocumentsAreSkipped(t *testing.T) {
	t.Parallel()
	second := "---\nopenapi: 3.1.0\ninfo: {title: N, version: \"1\"}\npaths: {}\n"

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf("---\n---\n"+minimal31), Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "T", doc.Doc.Info.GetTitle())
	assert.False(t, diag.HasError(diags), "nothing was dropped: %+v", diags)

	_, diags, err = Load(t.Context(), 0, openapitest.SourceOf("---\n---\n"+minimal31+second), Options{})
	require.NoError(t, err)
	got := streamDiags(diags)
	require.Len(t, got, 1)
	assert.Contains(t, got[0].Message, "the one after it holds", "the empties before are not counted")
}
