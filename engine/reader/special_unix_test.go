//go:build unix

package reader_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/reader"
)

// readBounded reads req through a Disk and fails the test, rather than hang
// it, when the read has not returned within ten seconds.
func readBounded(t *testing.T, req compilers.DocumentRequest) ([]byte, error) {
	t.Helper()
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, err := reader.Disk{}.ReadDocument(t.Context(), req)
		done <- result{data, err}
	}()
	select {
	case got := <-done:
		return got.data, got.err
	case <-time.After(10 * time.Second):
		t.Fatal("the read blocked")
		return nil, nil
	}
}

// TestDisk_ReadDocumentRefusesAFIFOWithoutBlocking pins that a FIFO is refused
// at once, whether the read is confined or not and by path or by file URI:
// opening one for reading waits for a writer, which nothing in a compile is,
// and no context interrupts the wait. The bound makes a regression fail here
// rather than hang the suite.
func TestDisk_ReadDocumentRefusesAFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	dir := resolvedDir(t)
	fifo := filepath.Join(dir, "p.yaml")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	t.Cleanup(func() {
		// Opened for reading and writing, a FIFO does not wait: this frees a
		// read a regression left waiting for a writer.
		f, err := os.OpenFile(fifo, os.O_RDWR, 0)
		if assert.NoError(t, err) {
			assert.NoError(t, f.Close())
		}
	})
	tests := []struct {
		name string
		req  compilers.DocumentRequest
	}{
		{name: "by path", req: compilers.DocumentRequest{Location: fifo}},
		{name: "by file URI", req: compilers.DocumentRequest{Location: fileURI(fifo)}},
		{name: "confined", req: compilers.DocumentRequest{Location: fifo, Within: dir}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := readBounded(t, tc.req)

			require.Error(t, err)
			assert.ErrorIs(t, err, reader.ErrNotRegular)
			assert.Contains(t, err.Error(), fifo)
			assert.Nil(t, got)
		})
	}
}

// TestDisk_ReadDocumentRefusesADevice pins the other special file a path can
// name: a character device, which reads without end or as nothing, is refused
// like a FIFO rather than read as a document, confined or not.
func TestDisk_ReadDocumentRefusesADevice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		req  compilers.DocumentRequest
	}{
		{name: "unconfined", req: compilers.DocumentRequest{Location: "/dev/null"}},
		{name: "confined", req: compilers.DocumentRequest{Location: "/dev/null", Within: "/dev"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := reader.Disk{}.ReadDocument(t.Context(), tc.req)

			require.Error(t, err)
			assert.ErrorIs(t, err, reader.ErrNotRegular)
			assert.Contains(t, err.Error(), "/dev/null")
			assert.Nil(t, got)
		})
	}
}
