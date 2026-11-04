// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package scan

import (
	"math"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// maxPointerAmplification bounds how many times the bytes a document writes
// the pointers naming its nodes may total (sourceindex.Index.PointerBytes). A
// node's pointer holds every key above it, and a compile spells one for each
// node it reads, so long keys nested deep cost gigabytes inside every other
// bound (GitHub #931).
//
// The highest ratio among the 2,336 specs the real-world sweep and the
// repository's own testdata read in November 2026 is 36.6, a conformance case
// writing a 250-byte path; Cloudflare's measures 8.9 and Stripe's 8.7. 128 is
// the margin maxAliasAmplification keeps.
const maxPointerAmplification = 128

// minPointerBytes is the pointer bytes granted whatever a document's size, so
// a small one nesting deep is never refused on its ratio: a document of short
// keys nested to MaxNesting spells about 16 MiB.
const minPointerBytes = 1 << 26 // 64 MiB

// PointerAmplification refuses a document whose nodes' pointers total past what its size
// allows (pointerAllowance) before the parser builds it, as a
// diag.PointerAmplification error at the document, or reports nothing for one
// within it. The index must cover the tree the parser will be handed,
// untruncated, and a document holding a recursive anchor is left to the
// anchor-cycle refusal, as Nesting leaves it.
func PointerAmplification(locate Locator, idx sourceindex.Index) (ir.Diagnostic, bool) {
	return pointerAmplificationWithin(locate, idx, pointerAllowance(idx.Written()))
}

// pointerAmplificationWithin is PointerAmplification under an allowance of allowance bytes, so a test
// can cross it without a document spelling the floor.
func pointerAmplificationWithin(locate Locator, idx sourceindex.Index, allowance int) (ir.Diagnostic, bool) {
	if idx.PointerBytes() <= allowance || len(idx.AnchorCycles()) > 0 {
		return ir.Diagnostic{}, false
	}
	return diag.Newf(ir.SeverityError, diag.PointerAmplification, locate(nil),
		"the pointers naming this document's nodes total %d bytes, past the %d-byte allowance "+
			"for a document writing %d bytes", idx.PointerBytes(), allowance, idx.Written()), true
}

// pointerAllowance is the pointer bytes a document writing written bytes may
// spell: maxPointerAmplification times them, floored at minPointerBytes and
// held at math.MaxInt.
func pointerAllowance(written int) int {
	if written > math.MaxInt/maxPointerAmplification {
		return math.MaxInt
	}
	return max(maxPointerAmplification*written, minPointerBytes)
}
