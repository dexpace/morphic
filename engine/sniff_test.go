package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/frontend"
)

func TestSniff_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      frontend.SourceFormat
		wantErr   string
	}{
		{"openapi 3.1 yaml", "openapi: 3.1.0\ninfo: {}\n", frontend.SourceFormat{Name: "openapi", Version: "3.1"}, ""},
		{"openapi 3.0 json", `{"openapi": "3.0.3"}`, frontend.SourceFormat{Name: "openapi", Version: "3.0"}, ""},
		{"swagger", "swagger: \"2.0\"\n", frontend.SourceFormat{}, "swagger 2.0 is not supported yet"},
		{"unknown", "hello: world\n", frontend.SourceFormat{}, "unrecognized spec format"},
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
