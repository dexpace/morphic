package irfacts_test

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main.
// TestIndex_EveryMethodIsSafeForConcurrentUse, in index_test.go, starts
// goroutines with sync.WaitGroup.Go.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
