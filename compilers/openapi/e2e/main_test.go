// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main. The amplification
// guards in amplification_test.go bound a compile with a buffered channel and
// a select on time.After; a channel that lost its buffer would leave that
// goroutine blocked forever with nothing to report it.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
