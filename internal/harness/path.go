package harness

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/reader"
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
	specs, err := SpecFiles(ctx, path)
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

// SpecFiles lists the spec files at path in filepath.WalkDir's lexical order. A
// regular file is listed itself. A directory is walked, its dot-directories
// skipped, and each regular file in it other than an IR snapshot (a name
// containing ".golden.") is read, and listed when a registered compiler takes
// it, recognizes a format it names, or declines it with a diagnostic, as it does
// a broken spec of its own. The rest are not specs. A Go error reports a nil
// context, an empty or unreadable path, or a failed walk.
func SpecFiles(ctx context.Context, path string) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("harness: nil context")
	}
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

	reg, err := newRegistry()
	if err != nil {
		return nil, err
	}
	return listDir(ctx, reg, path)
}

// listDir walks dir recursively and returns every file reg takes, in WalkDir's
// lexical visit order. The root carries a trailing separator because WalkDir
// does not follow a link at its root without one, and a link to a directory
// would otherwise be passed over as a file that is not a spec.
func listDir(ctx context.Context, reg *compilers.Registry, dir string) ([]string, error) {
	var files []string
	root := dir + string(filepath.Separator)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		spec, err := visit(ctx, reg, root, p, d)
		if spec {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("harness: walk %s: %w", dir, err)
	}
	return files, nil
}

// visit decides one walk entry under root: a dot-directory below root is skipped
// whole, any other directory is entered, and a file is a spec when reg takes it.
// The error is fs.SkipDir for a skipped directory, which WalkDir reads as
// instruction rather than failure.
func visit(ctx context.Context, reg *compilers.Registry, root, p string, d fs.DirEntry) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d.IsDir() {
		if p != root && strings.HasPrefix(d.Name(), ".") {
			return false, fs.SkipDir
		}
		return false, nil
	}

	regular, err := isRegular(p, d)
	if err != nil || !regular || isSnapshot(d.Name()) {
		return false, err
	}
	return isSpec(ctx, reg, p)
}

// isRegular reports whether the entry at p is a regular file or a link to one.
// A link is followed because Check reads through it; one whose target is gone
// is a fault of the tree and an error, not a file to pass over. Anything else (a
// socket, a pipe, a device) is not read at all, since reading may never return.
func isRegular(p string, d fs.DirEntry) (bool, error) {
	if d.Type().IsRegular() {
		return true, nil
	}
	if d.Type()&fs.ModeSymlink == 0 {
		return false, nil
	}
	info, err := os.Stat(p)
	if err != nil {
		return false, fmt.Errorf("harness: read %s: %w", p, err)
	}
	return info.Mode().IsRegular(), nil
}

// isSnapshot reports whether name is an IR snapshot, which holds a compiled
// document and not a source.
func isSnapshot(name string) bool {
	return strings.Contains(name, ".golden.")
}

// newRegistry registers the compilers into the registry SpecFiles asks whether
// a file is a spec. Each call builds a fresh one, so no listing shares a
// registry with another. An empty set is refused: every file would then read
// as no spec, and a sweep of the tree would pass having checked nothing.
func newRegistry() (*compilers.Registry, error) {
	cs := registered()
	if len(cs) == 0 {
		return nil, errors.New("harness: no compilers registered")
	}

	reg := compilers.NewRegistry()
	for i, c := range cs {
		if err := reg.Register(c); err != nil {
			return nil, fmt.Errorf("harness: register compiler %d: %w", i, err)
		}
	}
	return reg, nil
}

// sweepOptions is what SpecFiles asks every compiler whether it takes a file
// under: the options a run of the engine detects a spec with when the caller
// names none, its defaults, reading what a source's references name from disk.
func sweepOptions(compilers.Compiler) (compilers.Options, error) {
	return compilers.Options{Reader: reader.Disk{}}, nil
}

// readSpecFile reads a file listing asks the compilers about. It is a seam over
// os.ReadFile, as encodeYAML is over yaml.Marshal: a regular file that stat
// lists and the open then refuses is a fault no fixture can provoke without
// permission bits, which root ignores, so a test replaces it.
var readSpecFile = os.ReadFile

// isSpec reads the file at p and reports whether reg has a claim on it: a
// compiler takes it, one recognizes a format it names but none serves, or one
// declines it with a diagnostic, which is how a compiler reports a source that
// is its own and broken. Detection of a source no compiler recognizes is
// silent, and such a file is something else.
func isSpec(ctx context.Context, reg *compilers.Registry, p string) (bool, error) {
	data, err := readSpecFile(p)
	if err != nil {
		return false, fmt.Errorf("harness: read %s: %w", p, err)
	}
	det, ok, err := reg.Detect(ctx, compilers.Source{Path: p, Data: data}, sweepOptions)
	if err != nil {
		return false, fmt.Errorf("harness: detect %s: %w", p, err)
	}
	return ok || det.Recognition.Format.Name != "" || len(det.Declined) > 0, nil
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
