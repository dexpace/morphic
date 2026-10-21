package reader

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dexpace/morphic/compilers"
)

// Disk reads documents from the local file system and over http(s). The zero
// value is ready to use and safe for concurrent reads.
//
// A request confined to a directory follows the path's symbolic links, refuses
// one that leads outside, and opens the file they lead to through an os.Root
// at the directory, so a link swapped in after the check cannot lead outside
// either. Only a regular file is read, judged before it is opened, since
// opening a FIFO waits for a writer and no context interrupts that wait.
type Disk struct {
	// Client fetches http(s) URLs. Nil uses http.DefaultClient.
	Client *http.Client
}

// errNotRegular reports a file that is not a regular file: a directory, a
// device, a FIFO or a socket.
var errNotRegular = errors.New("not a regular file")

// ReadDocument implements compilers.DocumentReader.
func (d Disk) ReadDocument(ctx context.Context, req compilers.DocumentRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loc, err := locate(req, runtime.GOOS == "windows")
	if err != nil {
		return nil, err
	}
	switch {
	case loc.url != "":
		return d.fetch(ctx, loc.url, req.Limit)
	case req.Within != "":
		return readWithin(req.Within, loc.path, req.Limit)
	default:
		return readFile(loc.path, req.Limit)
	}
}

// fetch reads the document at the http(s) URL u, failing on any status but a
// success.
func (d Disk) fetch(ctx context.Context, u string, limit int) ([]byte, error) {
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err // a *url.Error, naming the request and its URL
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.Join(fmt.Errorf("GET %s: %s", u, resp.Status), resp.Body.Close())
	}
	data, err := readLimited(u, resp.Body, limit)
	if err = errors.Join(err, resp.Body.Close()); err != nil {
		return nil, err
	}
	return data, nil
}

// readWithin reads the file at path, which must lie inside within, through an
// os.Root opened where resolveWithin places it.
func readWithin(within, path string, limit int) ([]byte, error) {
	dir, rel, err := resolveWithin(within, path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err // a *fs.PathError, naming the directory
	}
	data, err := readRooted(root, rel, path, limit)
	if err = errors.Join(err, root.Close()); err != nil {
		return nil, err
	}
	return data, nil
}

// resolveWithin returns the directory to open within at and path relative to
// it, both with every symbolic link resolved, so the open names the file a
// link leads to rather than following the link. A path outside within, as
// written or once its links are followed, is refused with ErrOutsideWithin. A
// path whose links do not resolve, a missing file among them, is placed as
// written and left for the open to report. The two are put alike first, since
// a relative path's links resolve only below a working directory that may
// itself be reached through one.
func resolveWithin(within, path string) (string, string, error) {
	dir, file, rel, ok := relate(within, path)
	if !ok {
		return "", "", outside(path, within)
	}
	realDir, dirResolved := linksResolved(dir)
	realFile, fileResolved := linksResolved(file)
	if !dirResolved || !fileResolved {
		return dir, rel, nil
	}
	if rel, ok = relativeTo(realDir, realFile); !ok {
		return "", "", outside(path, within)
	}
	return realDir, rel, nil
}

// linksResolved returns p with every symbolic link resolved, and whether it
// resolved: a missing file and a dangling link do not.
func linksResolved(p string) (string, bool) {
	r, err := filepath.EvalSymlinks(p)
	return r, err == nil
}

// readRooted reads the file at rel inside root, which path names outside it. A
// failed open is reported against path, as an unconfined read reports it,
// rather than against rel and the call os.Root made.
func readRooted(root *os.Root, rel, path string, limit int) ([]byte, error) {
	if info, err := root.Stat(rel); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, errNotRegular)
	}
	f, err := root.Open(rel)
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		err = &fs.PathError{Op: "open", Path: path, Err: pe.Err}
	}
	if err != nil {
		return nil, err // a *fs.PathError, naming the file
	}
	return readOpened(path, f, limit)
}

// readFile reads the file at path, wherever it lies. A path Stat cannot read
// is left to os.Open, whose error names what is wrong with it.
func readFile(path string, limit int) ([]byte, error) {
	if info, err := os.Stat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, errNotRegular)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err // a *fs.PathError, naming the file
	}
	return readOpened(path, f, limit)
}

// readOpened reads f, opened as the file at path, and closes it. It judges f
// again, since the path can name another file by the time it is opened. A FIFO
// swapped in before the open still blocks it: only a process racing the
// compile can do that, never a spec, and the non-blocking open that would not
// wait is missing on the wasm ports.
func readOpened(path string, f fs.File, limit int) ([]byte, error) {
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s: %w", path, errNotRegular)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	data, err := readLimited(path, f, limit)
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, err
	}
	return data, nil
}
