// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// staleAfter is how old a temporary file must be before it is taken for what a
// killed Store left. A younger one may be another process's Store, whose rename
// would fail were it removed, so it is neither counted nor touched.
const staleAfter = time.Hour

// ownName matches the files a Dir may remove: an entry, and what an interrupted
// atomic replace of one left, a dot before its name and digits after. Nothing
// else is touched, so a directory shared with other files loses none of them.
var ownName = regexp.MustCompile(`^\.?[0-9a-f]{64}` + regexp.QuoteMeta(entrySuffix) + `[0-9]*$`)

// held is one file a Dir owns.
type held struct {
	name     string
	size     int
	modified time.Time
}

// prune removes d's own files unused longest, as of now, until the rest fit its
// limit. Another process may prune alongside, so a file already gone is not a
// failure.
func (d *Dir) prune(now time.Time) error {
	listed, err := d.ops.list(d.path)
	if err != nil {
		return fmt.Errorf("prune: %w", err)
	}
	files, err := heldIn(listed, now)
	if err != nil {
		return err
	}
	total := 0
	for _, f := range files {
		total += f.size
	}
	for _, f := range files {
		if total <= d.limit {
			return nil
		}
		if err := d.ops.remove(filepath.Join(d.path, f.name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("prune: %w", err)
		}
		total -= f.size
	}
	return nil
}

// heldIn returns the regular files of listed a Dir owns and may remove at now,
// unused longest first, ties in name order so two runs agree.
func heldIn(listed []fs.DirEntry, now time.Time) ([]held, error) {
	files := make([]held, 0, len(listed))
	for _, entry := range listed {
		if !entry.Type().IsRegular() || !ownName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("prune: %w", err)
		}
		if strings.HasPrefix(entry.Name(), ".") && now.Sub(info.ModTime()) < staleAfter {
			continue
		}
		files = append(files, held{name: entry.Name(), size: int(info.Size()), modified: info.ModTime()})
	}
	slices.SortFunc(files, func(a, b held) int {
		return cmp.Or(a.modified.Compare(b.modified), strings.Compare(a.name, b.name))
	})
	return files, nil
}
