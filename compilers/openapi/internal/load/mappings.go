// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"cmp"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// Mapping puts the documents under a URI prefix at a location, as the caller
// writes it (openapi.Location): the URL Prefix + rest is read at Location +
// rest. Prefix is an absolute URI ending in "/", spelled as resolve.Locate
// spells a located URL; Location is a directory, a relative one read against
// the source's, or an http(s) URL ending in "/".
type Mapping struct {
	Prefix   string
	Location string
}

// mapped returns where the located URL named lies under the longest prefix
// the caller mapped that starts it, true where one does, and why it names no
// location there: a segment of its rest that decodes to text holding a
// separator, or to a dot segment, which would climb out of the location
// (docs/compilers/openapi.md#references). Nothing is mapped by inference.
func (s sourceDocument) mapped(named string) (string, bool, error) {
	var best Mapping
	for _, m := range s.mappings {
		if strings.HasPrefix(named, m.Prefix) && len(m.Prefix) > len(best.Prefix) {
			best = m
		}
	}
	if best.Prefix == "" {
		return "", false, nil
	}
	rest := named[len(best.Prefix):]
	if refscope.IsURL(best.Location) {
		return best.Location + rest, true, nil
	}
	dir := filepath.Clean(best.Location)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(filepath.Dir(s.path), dir)
	}
	segments := strings.Split(rest, "/")
	for i, seg := range segments {
		decoded, err := url.PathUnescape(seg)
		if err != nil || strings.ContainsAny(decoded, `/\`) || decoded == "." || decoded == ".." {
			return "", true, fmt.Errorf(`its URL maps to no location: the segment "%s" holds an escaped separator`,
				cmp.Or(decoded, seg))
		}
		segments[i] = decoded
	}
	return filepath.Join(append([]string{dir}, segments...)...), true, nil
}
