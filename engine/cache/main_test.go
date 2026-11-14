// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/dexpace/morphic/engine/cache"
	"github.com/dexpace/morphic/internal/leakcheck"
)

// asBuildNamer, set in its environment, makes this test binary name its build
// in place of running the tests, once a line on stdin says to.
const asBuildNamer = "MORPHIC_TEST_AS_BUILD_NAMER"

// TestMain names the build for a child a BuildID test starts. Otherwise it runs
// the tests through leakcheck.Main.
func TestMain(m *testing.M) {
	if os.Getenv(asBuildNamer) != "" {
		nameBuild()
		os.Exit(0)
	}
	leakcheck.Main(m)
}

// nameBuild says it has started, waits for a line on stdin, and then writes
// what BuildID returns: the build's name, or the error behind "error: ". The
// wait lets a test replace the executable once the process runs it.
func nameBuild() {
	fmt.Println("started")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		fmt.Println("error: read stdin:", err)
		return
	}
	id, err := cache.BuildID(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(id)
}
