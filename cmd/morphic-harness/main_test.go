package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const okSpec = `openapi: 3.0.0
info: {title: t, version: "1"}
paths: {}
`

const badSpec = `openapi: 3.1.0
info: {title: Broken, version: "1"}
paths: {}
components:
  responses:
    Bad:
      headers:
        X:
          schema: {type: string}
          required: notabool
`

func writeSpec(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func TestRun_AllOKExitsZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpec(t, dir, "a.yaml", okSpec)
	writeSpec(t, dir, "b.yaml", okSpec)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{dir}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Contains(t, stdout.String(), "a.yaml")
	assert.Contains(t, stdout.String(), "b.yaml")
}

func TestRun_AnyFailureExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpec(t, dir, "good.yaml", okSpec)
	writeSpec(t, dir, "bad.yaml", badSpec)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{dir}, &stdout, &stderr)

	assert.Equal(t, 1, code, "a failing spec must gate the exit code")
}

func TestRun_MissingPathExitsTwo(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{filepath.Join(t.TempDir(), "nope")}, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "morphic-harness:")
}

func TestRun_NoArgsExitsTwo(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), nil, &stdout, &stderr)

	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "usage")
}

func TestMain_ExitCode(t *testing.T) {
	origArgs, origExit := os.Args, osExit
	t.Cleanup(func() { os.Args, osExit = origArgs, origExit })

	var got int
	osExit = func(code int) { got = code }
	os.Args = []string{"morphic-harness"} // no path args → usage → exit 2

	main()

	assert.Equal(t, 2, got)
}
