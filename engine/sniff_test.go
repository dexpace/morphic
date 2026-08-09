package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/ir"
)

func TestSniff_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      compilers.SourceFormat
		wantCode  string
	}{
		{"openapi 3.1 yaml", "openapi: 3.1.0\ninfo: {}\n", compilers.SourceFormat{Name: "openapi", Version: "3.1"}, ""},
		{"openapi 3.0 json", `{"openapi": "3.0.3"}`, compilers.SourceFormat{Name: "openapi", Version: "3.0"}, ""},
		{"openapi 3.2 yaml", "openapi: 3.2.0\ninfo: {}\n", compilers.SourceFormat{Name: "openapi", Version: "3.2"}, ""},
		// A version already in major.minor form (single dot) exercises majorMinor's
		// unchanged-passthrough return.
		{"openapi major.minor only", "openapi: \"3.1\"\n", compilers.SourceFormat{Name: "openapi", Version: "3.1"}, ""},
		// A bare-major version (no dot) also reaches the passthrough return. Sniff
		// reports what the source declared and judges none of it; an out-of-range
		// version is a spec problem the registry lookup names, not this step's.
		{"openapi bare major", "openapi: \"4\"\n", compilers.SourceFormat{Name: "openapi", Version: "4"}, ""},
		{"swagger", "swagger: \"2.0\"\n", compilers.SourceFormat{}, "engine/unsupported-format"},
		{"unknown", "hello: world\n", compilers.SourceFormat{}, "engine/unrecognized-format"},
		{"undecodable yaml", "openapi: [unterminated\n", compilers.SourceFormat{}, "engine/undecodable-source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diag, ok := engine.Sniff([]byte(tc.src))

			if tc.wantCode != "" {
				require.False(t, ok, "a source Sniff cannot read a format from is not ok")
				assert.Equal(t, tc.wantCode, diag.Code)
				assert.Equal(t, ir.SeverityError, diag.Severity)
				assert.NotEmpty(t, diag.Message, "a diagnostic has to say what is wrong")
				assert.Equal(t, tc.want, got, "no format is reported alongside a refusal")
				return
			}
			require.True(t, ok, "diag: %+v", diag)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, ir.Diagnostic{}, diag, "a format that was read leaves nothing to report")
		})
	}
}
