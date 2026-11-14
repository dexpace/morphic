// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package main

import (
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis"

	"github.com/dexpace/morphic/internal/exectest"
	"github.com/dexpace/morphic/internal/lintrules"
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
	cmd := exectest.Command(t.Context(), t, []string{asTool + "=1"}, os.Args[0], "-flags")
	out, err := cmd.Output()
	require.NoError(t, err, "the tool did not answer -flags")

	var flags []struct{ Name string }
	require.NoError(t, json.Unmarshal(out, &flags))
	names := make([]string, 0, len(flags))
	for _, f := range flags {
		names = append(names, f.Name)
	}
	assert.Subset(t, names,
		[]string{"fragmentreadings", "globalwrites", "idderivations", "namechannels", "registrywrites", "servicetreewrites"})
}

// TestMain_RunsTheToolOnTheRules runs main in this process, with vet standing
// in for unitchecker, and holds it to handing the tool every rule lintrules
// has. A child's coverage is discarded, so this is what covers main.
func TestMain_RunsTheToolOnTheRules(t *testing.T) {
	orig := vet
	t.Cleanup(func() { vet = orig })
	var got []string
	vet = func(analyzers ...*analysis.Analyzer) {
		for _, a := range analyzers {
			got = append(got, a.Name)
		}
	}

	main()

	analyzers := lintrules.Analyzers()
	want := make([]string, 0, len(analyzers))
	for _, a := range analyzers {
		want = append(want, a.Name)
	}
	require.NotEmpty(t, want)
	assert.Equal(t, want, got)
}
