// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dexpace/morphic/compilers"
)

var (
	_ compilers.DocumentReader   = Disk{}
	_ compilers.DirectoryLocator = Disk{}
)

// Disk reads documents from the local file system and over http(s). The zero
// value is ready to use and safe for concurrent reads.
//
// A request confined to a directory follows the path's symbolic links, refuses
// one that leads outside, and opens the file they lead to through an os.Root
// at the directory, so a link swapped in after the check cannot lead outside
// either. Only a regular file is read, judged before it is opened, since
// opening a FIFO waits for a writer and no context interrupts that wait. One
// sized past the limit is refused unread.
type Disk struct {
	// Client fetches http(s) URLs. Nil uses http.DefaultClient, each read
	// bounded in time (fetchTimeout). Every read goes through a copy whose
	// transport keeps no connection and judges each address it dials
	// (guardedClient), which needs Client's transport to be nil or an
	// *http.Transport dialling through no function of its own.
	Client *http.Client
	// classify ranks an address dialled; nil ranks it by spaceOf.
	classify func(netip.AddrPort) compilers.AddressSpace
	// resolver resolves the names a read dials; nil is net's default.
	resolver *net.Resolver
}

// ErrNotRegular reports a file that is not a regular file: a directory, a
// device, a FIFO or a socket. Opening a FIFO waits for a writer, and a device
// can stream without end, so neither is read as a document.
var ErrNotRegular = errors.New("not a regular file")

// LocateDirectory implements compilers.DirectoryLocator.
func (Disk) LocateDirectory(dir string) string { return locateDirectory(dir) }

// ReadDocument implements compilers.DocumentReader.
func (d Disk) ReadDocument(ctx context.Context, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	if err := ctx.Err(); err != nil {
		return compilers.DocumentRead{}, err
	}
	loc, err := locate(req, runtime.GOOS == "windows")
	if err != nil {
		return compilers.DocumentRead{}, err
	}
	if loc.url != "" {
		return d.fetch(ctx, loc.url, req)
	}
	var data []byte
	switch {
	case req.Within != "":
		data, err = readWithin(ctx, req.Within, loc.path, req.Limit)
	default:
		data, err = readFile(ctx, osFiles{}, loc.path, req.Limit)
	}
	if err != nil {
		return compilers.DocumentRead{}, err
	}
	return compilers.DocumentRead{Data: data}, nil
}

// fetch reads the document at the http(s) URL u, failing on any status but a
// success, through a client judging each address the read dials against
// req.Reach (guardedClient), and states the space the answer came from.
func (d Disk) fetch(ctx context.Context, u string, req compilers.DocumentRequest) (compilers.DocumentRead, error) {
	client, g, err := guardedClient(d.client(), req.Reach, d.classifier(), cmp.Or(d.resolver, net.DefaultResolver))
	if err != nil {
		return compilers.DocumentRead{}, fmt.Errorf("%s: %w", u, err)
	}
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return compilers.DocumentRead{}, fmt.Errorf("%s: %w", u, err)
	}
	resp, err := client.Do(get)
	if err != nil {
		return compilers.DocumentRead{}, g.failed(err) // a *url.Error, naming the request and its URL
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return compilers.DocumentRead{}, errors.Join(fmt.Errorf("GET %s: %s", u, resp.Status), resp.Body.Close())
	}
	// The response's length is the server's word, so it sizes nothing: a
	// hostile one could ask for any allocation. A compressed body is counted
	// as it expands, since net/http hands it over decompressed.
	data, err := readLimited(ctx, u, resp.Body, req.Limit, 0)
	if err = errors.Join(err, resp.Body.Close()); err != nil {
		return compilers.DocumentRead{}, err
	}
	read := compilers.DocumentRead{Data: data}
	if g != nil {
		read.Space = compilers.AddressSpace(g.space.Load())
		read.Bound = read.Space
	}
	return read, nil
}

// client is the client d fetches through: Client, or a copy of
// http.DefaultClient bounded in time.
func (d Disk) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	c := *http.DefaultClient
	c.Timeout = fetchTimeout
	return &c
}

// classifier is how d ranks an address it dials.
func (d Disk) classifier() func(netip.AddrPort) compilers.AddressSpace {
	if d.classify != nil {
		return d.classify
	}
	return func(ap netip.AddrPort) compilers.AddressSpace { return spaceOf(ap.Addr()) }
}

// readWithin reads the file at path, which must lie inside within, through an
// os.Root opened where resolveWithin places it.
func readWithin(ctx context.Context, within, path string, limit int) ([]byte, error) {
	dir, rel, err := resolveWithin(within, path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err // a *fs.PathError, naming the directory
	}
	data, err := readRooted(ctx, root, rel, path, limit)
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
func readRooted(ctx context.Context, root *os.Root, rel, path string, limit int) ([]byte, error) {
	if info, err := root.Stat(rel); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	f, err := root.Open(rel)
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		err = &fs.PathError{Op: "open", Path: path, Err: pe.Err}
	}
	if err != nil {
		return nil, err // a *fs.PathError, naming the file
	}
	return readOpened(ctx, path, f, limit)
}

// files is the file system a path is read from: the operating system's, or a
// test's, which presents a FIFO or a device on any platform.
type files interface {
	Stat(name string) (fs.FileInfo, error)
	Open(name string) (fs.File, error)
}

// osFiles is the operating system's file system.
type osFiles struct{}

// Stat implements files.
func (osFiles) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }

// Open implements files. A failed open hands back no file, never a nil
// *os.File in the interface, which a caller would read as one.
func (osFiles) Open(name string) (fs.File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// readFile reads the file at path, wherever it lies, from fsys.
func readFile(ctx context.Context, fsys files, path string, limit int) ([]byte, error) {
	f, err := openRegular(fsys, path)
	if err != nil {
		return nil, err
	}
	return readOpened(ctx, path, f, limit)
}

// openRegular opens the file at path in fsys, refusing one that is not a
// regular file before opening it. A path Stat cannot read is left to the open,
// whose error names what is wrong with it.
func openRegular(fsys files, path string) (fs.File, error) {
	if info, err := fsys.Stat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	return fsys.Open(path) // a *fs.PathError, naming the file
}

// readOpened reads f, opened as the file at path, and closes it. A file whose
// size is past limit is refused unread. Past it, a file growing as it is read
// is refused one byte past the limit.
func readOpened(ctx context.Context, path string, f fs.File, limit int) ([]byte, error) {
	info, err := judgeOpened(path, f)
	if err == nil && limit > 0 && info.Size() > int64(limit) {
		err = tooLarge(path, limit)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	data, err := readLimited(ctx, path, f, limit, info.Size())
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, err
	}
	return data, nil
}

// judgeOpened returns what f, opened as the file at path, is, refusing a file
// that is not regular. It judges again what openRegular judged, since the path
// can name another file by the time it is opened. A FIFO swapped in before the
// open still blocks it: only a process racing the compile can do that, never a
// spec, and the non-blocking open that would not wait is missing on the wasm
// ports.
func judgeOpened(path string, f fs.File) (fs.FileInfo, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	return info, nil
}
