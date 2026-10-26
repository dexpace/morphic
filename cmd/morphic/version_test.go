package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestModuleVersion_Cases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"no build info", nil, "devel"},
		{"no module version", &debug.BuildInfo{}, "devel"},
		{"a build outside a checkout", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "devel"},
		{"a release", &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, "v1.2.3"},
		{"a checkout with changes",
			&debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261009160651-e0c9762651cc+dirty"}},
			"v0.0.0-20261009160651-e0c9762651cc+dirty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, moduleVersion(tt.info))
		})
	}
}

// TestRun_VersionNamesTheBuildAndTheIRSchema pins both lines: the build says
// which binary this is, and the schema says which IR its output follows. The
// build's own text is left to TestModuleVersion_Cases, since what a test binary
// is stamped with is the toolchain's to decide.
func TestRun_VersionNamesTheBuildAndTheIRSchema(t *testing.T) {
	t.Parallel()

	for _, spelling := range []string{"--version", "-v"} {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			require.Equal(t, 0, run(t.Context(), []string{spelling}, &stdout, &stderr), "stderr: %s", stderr.String())

			require.True(t, strings.HasSuffix(stdout.String(), "\n"),
				"output that ends mid-line leaves the shell prompt on it, got %q", stdout.String())
			lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
			require.Len(t, lines, 2, "got:\n%s", stdout.String())
			assert.True(t, strings.HasPrefix(lines[0], "morphic version "), lines[0])
			assert.Equal(t, "IR schema "+ir.IRVersion, lines[1])
			assert.Empty(t, stderr.String())
		})
	}
}
