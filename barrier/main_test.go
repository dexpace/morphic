// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package barrier_test

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main.
// PanicWithNoCallerFrame, in export_test.go, starts a goroutine to panic on a
// stack no test function is on.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
