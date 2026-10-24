package reader_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/engine/reader"
)

// TestFilePathOf_NamesTheLocalFileAFileURINames pins how a file URI names a
// local file (RFC 8089): decoded, with localhost read as no host, a drive
// letter's leading slash dropped only where paths are read as Windows reads
// them, and every URI naming no local file, or naming one ambiguously,
// refused with the reason.
func TestFilePathOf_NamesTheLocalFileAFileURINames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, uri string
		windows   bool
		want      string
		wantErr   string
	}{
		{name: "an empty authority", uri: "file:///a/b.yaml", want: "/a/b.yaml"},
		{name: "no authority", uri: "file:/a/b.yaml", want: "/a/b.yaml"},
		{name: "localhost", uri: "file://localhost/a/b.yaml", want: "/a/b.yaml"},
		{name: "localhost in any case", uri: "file://LocalHost/a/b.yaml", want: "/a/b.yaml"},
		{name: "a percent-encoded name", uri: "file:///a/b%20c.yaml", want: "/a/b c.yaml"},
		{name: "dot segments, left for the read to clean", uri: "file:///a/./c/../b.yaml", want: "/a/./c/../b.yaml"},
		{name: "a drive on Windows", uri: "file:///C:/a/b.yaml", windows: true, want: "C:/a/b.yaml"},
		{name: "a lower-case drive on Windows", uri: "file://localhost/c:/a", windows: true, want: "c:/a"},
		{name: "a drive elsewhere is a directory", uri: "file:///C:/a/b.yaml", want: "/C:/a/b.yaml"},
		{name: "no drive on Windows", uri: "file:///a/b.yaml", windows: true, want: "/a/b.yaml"},
		{name: "a digit is no drive", uri: "file:///1:/a", windows: true, want: "/1:/a"},
		{name: "a drive-relative path on Windows", uri: "file:///C:a.yaml", windows: true, wantErr: "drive is followed by no slash"},
		{name: "a bare drive on Windows", uri: "file:///C:", windows: true, wantErr: "drive is followed by no slash"},
		{name: "a drive-relative path elsewhere is a name", uri: "file:///C:a.yaml", want: "/C:a.yaml"},
		{name: "an escaped backslash elsewhere", uri: "file:///a%5Cb.yaml", want: `/a\b.yaml`},
		{name: "a relative path", uri: "file:a.yaml", wantErr: "names no absolute path"},
		{name: "no path", uri: "file://", wantErr: "names no absolute path"},
		{name: "another host", uri: "file://server/share/a.yaml", wantErr: `on host "server", not a local one`},
		{name: "localhost with a port", uri: "file://localhost:80/a.yaml", wantErr: `on host "localhost:80"`},
		{name: "a user", uri: "file://me@/a.yaml", wantErr: "user names no local file"},
		{name: "a user at localhost", uri: "file://me@localhost/a.yaml", wantErr: "user names no local file"},
		{name: "a query", uri: "file:///a.yaml?v=1", wantErr: "query names nothing"},
		{name: "an empty query", uri: "file:///a.yaml?", wantErr: "query names nothing"},
		{name: "an escaped slash", uri: "file:///a%2Fb.yaml", wantErr: "escaped slash"},
		{name: "an escaped slash in lower case", uri: "file:///a%2fb.yaml", wantErr: "escaped slash"},
		{name: "an escaped backslash on Windows", uri: "file:///a%5cb.yaml", windows: true, wantErr: "backslash is a separator"},
		{name: "a backslash on Windows", uri: `file:///a\\b.yaml`, windows: true, wantErr: "backslash is a separator"},
		{name: "a share", uri: "file:////server/share/a.yaml", wantErr: "two slashes"},
		{name: "a share on Windows", uri: "file:////server/share/a.yaml", windows: true, wantErr: "two slashes"},
		{name: "a share under localhost", uri: "file://localhost//server/share/a.yaml", wantErr: "two slashes"},
		{name: "a backslash share on Windows", uri: `file:///\\server\share\a.yaml`, windows: true,
			wantErr: "backslash is a separator"},
		{name: "a backslash after the root elsewhere", uri: `file:///\\server\a.yaml`, want: `/\\server\a.yaml`},
		{name: "two slashes after a drive on Windows", uri: "file:///C://a.yaml", windows: true, want: "C://a.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u, err := url.Parse(tc.uri)
			require.NoError(t, err)

			got, err := reader.FilePathOf(u, tc.windows)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Empty(t, got, "a refused URI names no path")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
