//go:build unix

package load

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExternal_OpenRefusesAFIFOWithoutBlocking pins GitHub #930's own case:
// opening a FIFO for reading waits for a writer, which nothing in a compile
// is, so a $ref naming one held the compile until it was killed. It is
// refused at once. The wait is bounded so that a regression fails here rather
// than hanging the suite.
func TestExternal_OpenRefusesAFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	fifo := filepath.Join(t.TempDir(), "p.yaml")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	refusal := make(chan error, 1)
	go func() {
		_, err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{})).Open(fifo)
		refusal <- err
	}()

	select {
	case err := <-refusal:
		require.Error(t, err)
		assert.Equal(t, "external document "+fifo+" refused: it is not a regular file", err.Error())
	case <-time.After(10 * time.Second):
		t.Fatal("opening the FIFO blocked")
	}
}

// TestExternal_OpenRefusesADevice pins the other special file a path can
// name: a character device, which reads without end or as nothing, is refused
// like a FIFO rather than read as a document.
func TestExternal_OpenRefusesADevice(t *testing.T) {
	t.Parallel()

	_, err := newExternal(&soa.OpenAPI{}, Options{}, newExternalReads(sourceDocument{})).Open("/dev/null")

	require.Error(t, err)
	assert.Equal(t, "external document /dev/null refused: it is not a regular file", err.Error())
}
