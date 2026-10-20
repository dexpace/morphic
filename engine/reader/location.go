package reader

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/dexpace/morphic/compilers"
)

// location is a request's document as a reader reads it: a URL to fetch, or a
// file path, a file URI having been read as the path it names.
type location struct {
	url  string
	path string
}

// errEmptyLocation reports a request naming no document.
var errEmptyLocation = errors.New("the request names no document")

// locate reads req.Location as a location, refusing an empty one, a file URI
// naming no local file (see filePathOf), and a URL a request confined to a
// directory may not reach. windows says file URIs are read as Windows reads
// them.
func locate(req compilers.DocumentRequest, windows bool) (location, error) {
	if req.Location == "" {
		return location{}, errEmptyLocation
	}
	u, ok := asURL(req.Location)
	if !ok {
		return location{path: filepath.Clean(req.Location)}, nil
	}
	if u.Scheme == "file" {
		path, err := filePathOf(u, windows)
		if err != nil {
			return location{}, fmt.Errorf("%s: %w", req.Location, err)
		}
		return location{path: filepath.Clean(path)}, nil
	}
	if req.Within != "" {
		return location{}, outside(req.Location, req.Within)
	}
	return location{url: req.Location}, nil
}

// asURL parses s as one of the URLs a reader reads: http, https or file. Any
// other scheme, a Windows drive letter among them, and anything url.Parse
// refuses leave the location a path.
func asURL(s string) (*url.URL, bool) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, false
	}
	switch u.Scheme {
	case "http", "https", "file":
		return u, true
	default:
		return nil, false
	}
}

// relativeTo returns path relative to dir, and whether path lies inside dir,
// read lexically: dot segments are removed first, as a URI reference's are, and
// the working directory is read only when one of the two is absolute and the
// other is not. dir itself lies inside dir.
func relativeTo(dir, path string) (string, bool) {
	dir, path = filepath.Clean(dir), filepath.Clean(path)
	if filepath.IsAbs(dir) != filepath.IsAbs(path) {
		var err error
		if dir, err = filepath.Abs(dir); err != nil {
			return "", false
		}
		if path, err = filepath.Abs(path); err != nil {
			return "", false
		}
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}
	return rel, true
}

// outside returns the error a request confined to within fails with for name.
func outside(name, within string) error {
	return fmt.Errorf("%s: %w %q", name, compilers.ErrOutsideWithin, within)
}

// readLimited reads r, no further than one byte past limit, and refuses what
// is past it. A limit of zero or less reads everything; at math.MaxInt no
// document can pass the limit, and the byte past it would overflow.
func readLimited(name string, r io.Reader, limit int) ([]byte, error) {
	if limit > 0 && limit < math.MaxInt {
		r = io.LimitReader(r, int64(limit)+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if limit > 0 && len(data) > limit {
		return nil, tooLarge(name, limit)
	}
	return data, nil
}

// tooLarge returns the error a document name larger than limit fails with.
func tooLarge(name string, limit int) error {
	return fmt.Errorf("%s: %w (%d bytes)", name, compilers.ErrDocumentTooLarge, limit)
}

// filePathOf returns the path of the local file the file URI u names (RFC
// 8089), percent-decoded, with a Windows drive letter's leading slash removed
// where windows says paths are read as Windows reads them. It refuses a URI
// naming a user, a file on another host, no absolute path or a query, a path
// opening with two slashes, which Windows reads as another host's share, a
// drive no slash follows, which Windows reads against that drive's working
// directory, and a separator that would decode into another segmentation: an
// escaped slash, or on Windows a backslash.
func filePathOf(u *url.URL, windows bool) (string, error) {
	switch {
	case u.Path == "": // an opaque URI, file:a.yaml, has none
		return "", errors.New("a file URI names no absolute path")
	case u.User != nil:
		return "", errors.New("a file URI's user names no local file")
	case u.Host != "" && !strings.EqualFold(u.Host, "localhost"):
		return "", fmt.Errorf("a file URI names a file on host %q, not a local one", u.Host)
	case u.RawQuery != "" || u.ForceQuery:
		return "", errors.New("a file URI's query names nothing in a file")
	}
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") {
		return "", errors.New("a file URI's escaped slash names no single path segment")
	}
	// EscapedPath escapes a backslash written as itself too.
	if windows && strings.Contains(escaped, "%5c") {
		return "", errors.New("a file URI's backslash is a separator on Windows, so it names no single path segment")
	}
	path := u.Path
	if windows && len(path) >= 3 && path[0] == '/' && path[2] == ':' && isDriveLetter(path[1]) {
		if !strings.HasPrefix(path[3:], "/") {
			return "", errors.New("a file URI's drive is followed by no slash, so it names no absolute path")
		}
		path = path[1:]
	}
	if strings.HasPrefix(path, "//") {
		return "", errors.New("a file URI's path opens with two slashes, naming a network share, not a local file")
	}
	return path, nil
}

// isDriveLetter reports whether c names a Windows drive.
func isDriveLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}
