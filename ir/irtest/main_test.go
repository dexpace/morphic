package irtest

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main. runCompare, in
// golden_internal_test.go, drives compareGolden on its own goroutine so a
// stubbed testingT's simulated Fatalf can call runtime.Goexit without
// unwinding the real test goroutine.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
