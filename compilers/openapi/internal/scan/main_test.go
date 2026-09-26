package scan

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main. scanWithin, in
// scan_internal_test.go, bounds Cycles with a buffered channel and a select on
// time.After; a channel that lost its buffer would leave that goroutine
// blocked forever with nothing to report it.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
