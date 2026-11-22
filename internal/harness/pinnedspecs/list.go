// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pinnedspecs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Spec is one spec the sweep reads in place: the name its record and its
// fetched copy go by, the GitHub repository and the commit it is read at, its
// path in that repository, and the SHA-256 of its bytes there.
type Spec struct {
	Name, Repository, Commit, Path, SHA256 string
}

// The forms of a Spec's fields. A name and a path become a path under the
// directory a fetch writes to, and a repository, a commit and a path a URL, so
// each is held to a form that can escape neither: no segment of a repository
// or a path is "." or "..", which a URL's reader collapses, and none holds a
// character a URL reads as other than itself, such as "?", "#" or "%".
var (
	namePattern       = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	segmentPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	commitPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ReadList reads a list of specs: a line per spec holding its name,
// repository, commit, path and SHA-256 in that order, separated by spaces or
// tabs, and a line starting "#" a comment. It reads only what
// scripts/fetch-pinned-specs.sh reads alike, so a line not ending in a newline,
// which the script skips, is an error, and so are a line of another shape, a
// field of another form (Spec.validate), a name listed twice and a list naming
// no spec.
func ReadList(data []byte) ([]Spec, error) {
	var specs []Spec
	seen := map[string]bool{}
	n := 0
	for line := range strings.Lines(string(data)) {
		n++
		text, ended := strings.CutSuffix(line, "\n")
		if !ended {
			return nil, fmt.Errorf("pinnedspecs: list line %d: want a newline at its end, "+
				"as the fetch script reads no line without one, got %q", n, line)
		}
		fields := strings.FieldsFunc(text, separator)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) != 5 {
			return nil, fmt.Errorf("pinnedspecs: list line %d: want name, repository, commit, path and SHA-256, got %q",
				n, text)
		}
		spec := Spec{Name: fields[0], Repository: fields[1], Commit: fields[2], Path: fields[3], SHA256: fields[4]}
		if err := spec.validate(); err != nil {
			return nil, fmt.Errorf("pinnedspecs: list line %d: %w", n, err)
		}
		if seen[spec.Name] {
			return nil, fmt.Errorf("pinnedspecs: list line %d: %q is listed twice", n, spec.Name)
		}
		seen[spec.Name] = true
		specs = append(specs, spec)
	}
	if len(specs) == 0 {
		return nil, errors.New("pinnedspecs: the list names no spec")
	}
	return specs, nil
}

// separator reports whether r separates a list line's fields: a space or a
// tab, the separators the fetch script's read splits on, and no other white
// space, such as a vertical tab or a no-break space.
func separator(r rune) bool {
	return r == ' ' || r == '\t'
}

// segmentForm says what segment holds a part to, for an error refusing one.
const segmentForm = "of letters, digits, ., _ and -, and neither . nor .."

// segment reports whether part is one segment of a path a URL names as
// written: characters of segmentPattern, and neither "." nor "..".
func segment(part string) bool {
	return segmentPattern.MatchString(part) && part != "." && part != ".."
}

// validate holds each of s's fields to its form: a name of lowercase words
// joined by "-", an owner and a repository, a full commit hash, a relative
// path of segments (segment), and a SHA-256 in hex.
func (s Spec) validate() error {
	if !namePattern.MatchString(s.Name) {
		return fmt.Errorf("name %q: want lowercase letters and digits, joined by -", s.Name)
	}
	if !repositoryPattern.MatchString(s.Repository) || !segment(path.Base(s.Repository)) {
		return fmt.Errorf("%s: repository %q: want owner/name, the name %s", s.Name, s.Repository, segmentForm)
	}
	if !commitPattern.MatchString(s.Commit) {
		return fmt.Errorf("%s: commit %q: want the full hash, in lowercase hex", s.Name, s.Commit)
	}
	if slices.ContainsFunc(strings.Split(s.Path, "/"), func(part string) bool { return !segment(part) }) {
		return fmt.Errorf("%s: path %q: want a relative path, each segment %s", s.Name, s.Path, segmentForm)
	}
	if !sha256Pattern.MatchString(s.SHA256) {
		return fmt.Errorf("%s: SHA-256 %q: want 64 lowercase hex digits", s.Name, s.SHA256)
	}
	return nil
}

// File is where a fetch into dir writes s: the file of s's path's own name, in
// a directory named for s, so two specs whose files share a name keep apart.
func (s Spec) File(dir string) string {
	return filepath.Join(dir, s.Name, path.Base(s.Path))
}

// Read returns the bytes of s's copy fetched into dir (File), refusing bytes
// whose SHA-256 is not the one s pins, as a copy of another revision or one
// altered since has, and a Spec of another form than ReadList returns.
func (s Spec) Read(dir string) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("pinnedspecs: %w", err)
	}
	if dir == "" {
		return nil, fmt.Errorf("pinnedspecs: %s: no directory to read it from", s.Name)
	}
	data, err := os.ReadFile(s.File(dir))
	if err != nil {
		return nil, fmt.Errorf("pinnedspecs: %w", err) // a *fs.PathError names the read and the path
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != s.SHA256 {
		return nil, fmt.Errorf("pinnedspecs: %s: %s has SHA-256 %s, where the list pins %s", s.Name, s.File(dir), got, s.SHA256)
	}
	return data, nil
}
