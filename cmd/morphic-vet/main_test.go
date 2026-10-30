// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"encoding/json/v2"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asTool makes this test binary run main in place of the tests. unitchecker
// exits the process itself on every path, so main is driven in a child.
const asTool = "MORPHIC_VET_AS_TOOL"

func TestMain(m *testing.M) {
	if os.Getenv(asTool) != "" {
		main()
		os.Exit(2) // main returned, which unitchecker never does; a child must not run the tests
	}
	os.Exit(m.Run())
}

// TestMain_HandsGoVetTheRules asks the tool, as go vet does before it hands it
// a package, which flags it takes. unitchecker gives each analyzer a flag of
// its name, so the answer names the rules main passes it.
func TestMain_HandsGoVetTheRules(t *testing.T) {
	t.Parallel()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-flags")
	cmd.Env = append(os.Environ(), asTool+"=1")
	out, err := cmd.Output()
	require.NoError(t, err, "the tool did not answer -flags")

	var flags []struct{ Name string }
	require.NoError(t, json.Unmarshal(out, &flags))
	names := make([]string, 0, len(flags))
	for _, f := range flags {
		names = append(names, f.Name)
	}
	assert.Subset(t, names,
		[]string{"fragmentreadings", "idderivations", "namechannels", "registrywrites", "servicetreewrites"})
}
