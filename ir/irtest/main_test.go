// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest_test

import (
	"testing"

	"github.com/dexpace/morphic/internal/leakcheck"
)

// TestMain runs this package's tests through leakcheck.Main. runCompare, in
// golden_test.go, drives compareGolden on its own goroutine so a stubbed
// testingT's simulated Fatalf can call runtime.Goexit without unwinding the
// real test goroutine.
func TestMain(m *testing.M) {
	leakcheck.Main(m)
}
