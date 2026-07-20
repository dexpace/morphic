package harness

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CheckPath applies Check to the spec(s) at path. When path is a directory it is
// walked recursively for *.yaml, *.yml, and *.json spec files (excluding
// *.golden.json IR snapshots), each read and checked; when path names a regular
// file it is read and checked directly, whatever its extension. Results are
// returned in the deterministic lexical order filepath.WalkDir visits entries.
//
// It returns a Go error only for a caller mistake or a filesystem fault — a nil
// context, an empty path, a missing path, an unreadable file. A spec that trips
// an oracle is a Result with a non-OK Outcome, never a Go error, so a broken
// spec never aborts a sweep of its siblings.
func CheckPath(ctx context.Context, path string) ([]Result, error) {
	if ctx == nil {
		return nil, fmt.Errorf("harness: nil context")
	}
	if path == "" {
		return nil, fmt.Errorf("harness: empty path")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("harness: stat %s: %w", path, err)
	}
	if !info.IsDir() {
		r, err := checkFile(ctx, path)
		if err != nil {
			return nil, err
		}
		return []Result{r}, nil
	}
	return checkDir(ctx, path)
}

// checkDir walks dir recursively and runs Check on every spec file it finds,
// returning results in WalkDir's lexical visit order.
func checkDir(ctx context.Context, dir string) ([]Result, error) {
	var results []Result
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isSpecFile(p) {
			return nil
		}
		r, cerr := checkFile(ctx, p)
		if cerr != nil {
			return cerr
		}
		results = append(results, r)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("harness: walk %s: %w", dir, err)
	}
	return results, nil
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
