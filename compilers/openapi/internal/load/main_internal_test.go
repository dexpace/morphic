// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main, for the tests that
// start goroutines: the source held under several spellings at once, one
// document read by several asks, and the open of a FIFO that must not block.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
