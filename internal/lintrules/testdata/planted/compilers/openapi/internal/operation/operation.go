// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package operation stands for a lowering, which reads what a reference stands
// for through the bindings, never through the library's reference wrapper.
package operation

import soa "github.com/speakeasy-api/openapi/openapi"

// plantResolution reads the library's resolution off a reference wrapper, each
// way refused, an alias's wrapper and a method value among them. Whether the
// wrapper writes a $ref, and what it writes, read no resolution.
func plantResolution(rp *soa.ReferencedParameter, rr *soa.ReferencedResponse) {
	_ = rp.GetObject()                  // want "use of `rp.GetObject` forbidden because \"read what a reference stands for through resolve.Object, which asks the bindings\" (forbidigo)"
	_ = rr.GetObject()                  // want "use of `rr.GetObject` forbidden because \"read what a reference stands for through resolve.Object, which asks the bindings\" (forbidigo)"
	_ = rp.IsResolved()                 // want "use of `rp.IsResolved` forbidden because \"read what a reference stands for through resolve.Object, which asks the bindings\" (forbidigo)"
	_ = rp.GetResolvedObject()          // want "use of `rp.GetResolvedObject` forbidden because \"read what a reference stands for through resolve.Object, which asks the bindings\" (forbidigo)"
	_ = rp.MustGetObject                // want "use of `rp.MustGetObject` forbidden because \"read what a reference stands for through resolve.Object, which asks the bindings\" (forbidigo)"
	_ = rp.GetObjectAny()               // want "use of `rp.GetObjectAny` forbidden because \"read what a reference stands for through resolve.Object, which asks the bindings\" (forbidigo)"
	_ = rp.GetReferenceResolutionInfo() // want "use of `rp.GetReferenceResolutionInfo` forbidden because \"read what a reference stands for through resolve.Object, which asks the bindings\" (forbidigo)"
	_, _ = rp.IsReference(), rp.GetReference()
}
