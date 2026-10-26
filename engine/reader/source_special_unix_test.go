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

	"github.com/dexpace/morphic/engine/reader"
)

// TestReadSource_RefusesARealFIFOAndDeviceWithoutBlocking runs the refusal on
// the operating system's own special files: a FIFO no process writes to, whose
// open would wait for one, and /dev/zero, which streams without end. Each is
// refused at once; the bound makes a regression fail rather than hang.
func TestReadSource_RefusesARealFIFOAndDeviceWithoutBlocking(t *testing.T) {
	t.Parallel()
	fifo := filepath.Join(reader.ResolvedDir(t), "spec.yaml")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	t.Cleanup(func() {
		// Opened for reading and writing, a FIFO does not wait: this frees a
		// read a regression left waiting for a writer.
		f, err := os.OpenFile(fifo, os.O_RDWR, 0)
		if assert.NoError(t, err) {
			assert.NoError(t, f.Close())
		}
	})
	for _, path := range []string{fifo, "/dev/zero"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			type result struct {
				data []byte
				err  error
			}
			done := make(chan result, 1)
			go func() {
				data, err := reader.ReadSource(t.Context(), path, 1<<20)
				done <- result{data, err}
			}()
			select {
			case got := <-done:
				require.ErrorIs(t, got.err, reader.ErrNotRegular)
				assert.Contains(t, got.err.Error(), path)
				assert.Nil(t, got.data)
			case <-time.After(10 * time.Second):
				t.Fatal("the read blocked")
			}
		})
	}
}
