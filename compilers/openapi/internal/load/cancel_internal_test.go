// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// liveFor returns a context over t's that answers left checks, then refuses.
func liveFor(t *testing.T, left int) *ctxtest.Countdown {
	t.Helper()
	return &ctxtest.Countdown{Context: t.Context(), Left: left}
}

// TestContextReader_FailsOnceTheContextIsDone pins the reader every decode goes
// through: it reads while its context is live, and once it is done fails with
// the context's error without reading.
func TestContextReader_FailsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	under := strings.NewReader("abcdef")
	r := contextReader{ctx: liveFor(t, 1), r: under}
	buf := make([]byte, 3)

	n, err := r.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(buf[:n]))

	n, err = r.Read(buf)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, n)
	assert.Equal(t, 3, under.Len(), "nothing more was read")
}

// bigSpec is a source the parser reads in many chunks, so a decode stopped by
// its context is seen to stop partway.
func bigSpec() string {
	return minimal31 + "x-pad:\n" + strings.Repeat("  - a value of some length to fill a chunk\n", 4096)
}

// TestDecode_StopsWithinAReadOnceTheContextIsDone pins that a parse reads its
// bytes through its context: cancelled after a few reads, it stops at the next
// and reports the context's error, where a parse run to its end asks hundreds
// of times.
func TestDecode_StopsWithinAReadOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	data := []byte(bigSpec())
	full := liveFor(t, 1<<30)
	_, err := Decode(full, data)
	require.NoError(t, err)
	require.Greater(t, full.Calls, 100, "the whole parse asks many times")

	ctx := liveFor(t, 3)
	parsed, err := Decode(ctx, data)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, parsed)
	assert.Equal(t, 5, ctx.Calls,
		"the parse stopped at the first read the context refused, the fourth, and its error read the context once more")
}

// TestDecode_ACancelInTheTailIsTheContexts pins the documents after the one
// lowered: a cancel landing while they are read stops the decode with the
// context's error, rather than leaving a tail read as one that did not parse.
func TestDecode_ACancelInTheTailIsTheContexts(t *testing.T) {
	t.Parallel()
	data := []byte(minimal31 + "---\n" + bigSpec())
	full := liveFor(t, 1<<30)
	parsed, err := Decode(full, data)
	require.NoError(t, err)
	require.Equal(t, 1, parsed.rest.dropped, "the stream holds a document past the one lowered")

	// The last ask is the check after the tail; the one before it is the
	// tail's last read.
	ctx := liveFor(t, full.Calls-2)
	parsed, err = Decode(ctx, data)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, parsed)
}

// TestDecode_ReadsAStreamWholeThroughALiveContext pins that reading through the
// context changes nothing a live one parses: the reader ends the stream as the
// bytes do.
func TestDecode_ReadsAStreamWholeThroughALiveContext(t *testing.T) {
	t.Parallel()
	r := contextReader{ctx: t.Context(), r: strings.NewReader(minimal31)}

	got, err := io.ReadAll(r)

	require.NoError(t, err)
	assert.Equal(t, minimal31, string(got))
}

// TestLoad_StopsOnceTheContextIsDoneAtTheParse pins the first place a load
// sees its context: the parse, whether it reads the bytes or takes the one
// detection made. Either way the load ends with the context's error and no
// document, where the source would otherwise load.
func TestLoad_StopsOnceTheContextIsDoneAtTheParse(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	parsed, err := Decode(t.Context(), []byte(minimal31))
	require.NoError(t, err)
	for name, parse := range map[string]any{"parsed by the load": nil, "parsed by detection": parsed} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := openapitest.SourceOf(minimal31)
			src.Parsed = parse
			live, _, err := Load(t.Context(), 0, src, Options{})
			require.NoError(t, err)
			require.NotNil(t, live, "live, the source loads")

			src.Parsed = parse
			doc, diags, err := Load(ctx, 0, src, Options{})

			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, doc)
			assert.Empty(t, diags)
		})
	}
}

// TestParseTree_StopsWithinAReadOnceTheContextIsDone pins the parse of an
// external document and of an overlay before it is applied: it reads through
// its context as the source's parse does, and parses bytes holding no document
// to an empty tree, as yaml.Unmarshal does, rather than failing.
func TestParseTree_StopsWithinAReadOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	data := []byte(bigSpec())
	full := liveFor(t, 1<<30)
	_, err := parseTree(full, data)
	require.NoError(t, err)
	require.Greater(t, full.Calls, 100)

	ctx := liveFor(t, 3)
	_, err = parseTree(ctx, data)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 5, ctx.Calls, "the parse stopped at the read the context refused")

	empty, err := parseTree(t.Context(), []byte("# nothing\n"))
	require.NoError(t, err)
	assert.Zero(t, empty.Kind, "no document is an empty tree")
}
