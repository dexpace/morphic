package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
)

func TestSniff_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      compilers.SourceFormat
		wantErr   string
	}{
		{"openapi 3.1 yaml", "openapi: 3.1.0\ninfo: {}\n", compilers.SourceFormat{Name: "openapi", Version: "3.1"}, ""},
		{"openapi 3.0 json", `{"openapi": "3.0.3"}`, compilers.SourceFormat{Name: "openapi", Version: "3.0"}, ""},
		{"openapi 3.2 yaml", "openapi: 3.2.0\ninfo: {}\n", compilers.SourceFormat{Name: "openapi", Version: "3.2"}, ""},
		// A version already in major.minor form (single dot) exercises majorMinor's
		// unchanged-passthrough return.
		{"openapi major.minor only", "openapi: \"3.1\"\n", compilers.SourceFormat{Name: "openapi", Version: "3.1"}, ""},
		// A bare-major version (no dot) also reaches the passthrough return.
		{"openapi bare major", "openapi: \"4\"\n", compilers.SourceFormat{Name: "openapi", Version: "4"}, ""},
		{"swagger", "swagger: \"2.0\"\n", compilers.SourceFormat{}, "swagger 2.0 is not supported yet"},
		{"unknown", "hello: world\n", compilers.SourceFormat{}, "unrecognized spec format"},
		{"undecodable yaml", "openapi: [unterminated\n", compilers.SourceFormat{}, "sniff: decode source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := engine.Sniff([]byte(tc.src))
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
