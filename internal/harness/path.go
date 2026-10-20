package harness

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CheckPath applies Check to each spec file SpecFiles lists at path, in the
// order it lists them.
//
// It returns a Go error only for a caller or filesystem fault (nil context,
// bad path, unreadable file) — an oracle failure is a non-OK Result, never a
// Go error, so one broken spec never aborts the sweep of its siblings.
func CheckPath(ctx context.Context, path string) ([]Result, error) {
	if ctx == nil {
		return nil, errors.New("harness: nil context")
	}
	specs, err := SpecFiles(path)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(specs))
	for _, spec := range specs {
		r, err := checkFile(ctx, spec)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// SpecFiles lists the spec files at path without reading any: a directory is
// walked recursively for *.yaml, *.yml, and *.json files (excluding
// *.golden.json IR snapshots) in filepath.WalkDir's lexical order; a regular
// file is listed itself, whatever its extension. It returns a Go error for an
// empty path, one that cannot be stat'ed, and a walk that fails.
func SpecFiles(path string) ([]string, error) {
	if path == "" {
		return nil, errors.New("harness: empty path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("harness: %w", err) // a *fs.PathError names the stat and the path
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	return listDir(path)
}

// listDir walks dir recursively and returns every spec file it finds, in
// WalkDir's lexical visit order.
func listDir(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && isSpecFile(p) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("harness: walk %s: %w", dir, err)
	}
	return files, nil
}

// checkFile reads one spec file and runs Check on its bytes, keying the Result
// on the file path. A read fault is a Go error; an oracle failure is not.
func checkFile(ctx context.Context, path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("harness: read %s: %w", path, err)
	}
	return Check(ctx, path, data), nil
}

// isSpecFile reports whether p names a spec input a directory sweep should
// check: a .yaml, .yml, or .json file that is not a .golden.json IR snapshot.
func isSpecFile(p string) bool {
	if strings.HasSuffix(p, ".golden.json") {
		return false
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}
