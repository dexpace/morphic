// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package cache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/engine/reader"
)

// ledgerName names the file each Store appends its entry's size to. Listing the
// directory costs a prune what the entries number, so a Store prunes only once
// the bytes stored since the last prune pass an eighth of the limit, or once
// maxRecords Stores have passed, and starts the ledger afresh.
const ledgerName = "stored.ledger"

// recordWidth is one ledger record: a size in twenty digits and a newline, one
// short write, which lands whole beside another process's.
const recordWidth = 21

// maxRecords bounds the ledger, so reading it stays cheap, and a prune comes at
// least once in that many Stores.
const maxRecords = 1 << 12

// ledger is the path of d's ledger.
func (d *Dir) ledger() string { return filepath.Join(d.path, ledgerName) }

// pruneDue records size in the ledger and reports whether a prune is due by what
// the ledger then holds (tallyDue). A ledger it cannot write or read is taken as
// due, so a fault costs a scan and never the bound.
func (d *Dir) pruneDue(ctx context.Context, size int) bool {
	if err := appendRecord(d.ledger(), size); err != nil {
		return true
	}
	data, err := reader.ReadSource(ctx, d.ledger(), maxRecords*recordWidth)
	return err != nil || tallyDue(data, d.limit)
}

// resetLedger removes the ledger ahead of a prune, so the Stores after it tally
// afresh. A ledger already gone, another process's prune having reset it, is
// no failure.
func (d *Dir) resetLedger() error {
	err := d.ops.remove(d.ledger())
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("reset the ledger: %w", err)
}

// tallyDue reports whether a ledger holding data calls for a prune under limit:
// it tallies more than an eighth of limit, or holds more than maxRecords records,
// or anything but whole records of sizes limit allows. A read that caught another
// process mid-write ends in part of a record, which is due, never a short record
// indexed past its end.
func tallyDue(data []byte, limit int) bool {
	if len(data) > maxRecords*recordWidth || len(data)%recordWidth != 0 {
		return true
	}
	total := 0
	for record := range slices.Chunk(data, recordWidth) {
		n, err := strconv.Atoi(string(record[:recordWidth-1]))
		if err != nil || n < 0 || n > limit || record[recordWidth-1] != '\n' {
			return true
		}
		total += n
	}
	return total > limit/8
}

// appendRecord appends size to the ledger at path, creating it if need be.
func appendRecord(path string, size int) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(fmt.Appendf(nil, "%0*d\n", recordWidth-1, size))
	return errors.Join(err, f.Close())
}
