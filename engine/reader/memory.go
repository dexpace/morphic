package reader

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/dexpace/morphic/compilers"
)

// Memory reads documents from sources held in memory, and nothing else: what
// Disk would read were each source a file at its Path. A file path or file URI
// finds the source whose Path, dot segments removed, names the same path; an
// http(s) URL the source whose Path is that URL. A confined request is judged
// lexically, as memory holds no symbolic link.
//
// Identity is lexical, as a reference's is: a relative and an absolute path
// naming one file are two documents. Memory is safe for concurrent reads.
type Memory struct {
	docs map[string][]byte
}

// NewMemory returns a Memory serving sources, each under its Path. A source
// with no Path cannot be named, so none is served; two sources whose Paths name
// one document are refused, since a read would have to choose between them.
// Memory keeps each source's bytes and never writes them, so they must not
// change while it serves them; every read is handed a copy of its own.
func NewMemory(sources []compilers.Source) (*Memory, error) {
	m := &Memory{docs: make(map[string][]byte, len(sources))}
	for i, src := range sources {
		if src.Path == "" {
			continue
		}
		key, err := memoryKey(src.Path)
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", i, err)
		}
		if _, dup := m.docs[key]; dup {
			return nil, fmt.Errorf("source %d: %q names a document another source names", i, src.Path)
		}
		m.docs[key] = src.Data
	}
	return m, nil
}

// memoryKey returns the key a source at path, or a request for it, is found
// under: a path with its dot segments removed, or a URL as written.
func memoryKey(path string) (string, error) {
	loc, err := locate(compilers.DocumentRequest{Location: path}, runtime.GOOS == "windows")
	if err != nil {
		return "", err
	}
	if loc.url != "" {
		return loc.url, nil
	}
	return loc.path, nil
}

// ReadDocument implements compilers.DocumentReader. A document no source holds
// fails as a file that does not exist does, with fs.ErrNotExist.
func (m *Memory) ReadDocument(ctx context.Context, req compilers.DocumentRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	loc, err := locate(req, runtime.GOOS == "windows")
	if err != nil {
		return nil, err
	}
	key := loc.url
	if key == "" {
		key = loc.path
		if req.Within != "" {
			if _, ok := relativeTo(req.Within, loc.path); !ok {
				return nil, outside(loc.path, req.Within)
			}
		}
	}
	data, ok := m.docs[key]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: key, Err: fs.ErrNotExist}
	}
	if req.Limit > 0 && len(data) > req.Limit {
		return nil, tooLarge(key, req.Limit)
	}
	return bytes.Clone(data), nil
}
