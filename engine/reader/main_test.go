// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader_test

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main.
// TestMemory_ConcurrentReadsAreSafe, in memory_test.go, fans reads out over
// sync.WaitGroup.Go, which starts a real goroutine per worker.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
