package engine_test

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main.
// TestEngine_ConcurrentRunSharesOneEngine, in concurrency_test.go, fans work
// out over sync.WaitGroup.Go, which starts a real goroutine per worker.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
