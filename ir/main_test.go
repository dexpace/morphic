package ir_test

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main.
// TestFacts_EveryMethodIsSafeForConcurrentUse, in facts_test.go, starts
// goroutines with sync.WaitGroup.Go.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
