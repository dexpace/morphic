// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package openapicases writes the OpenAPI compiler's schema cases (New): a
// product of version, target, construct, second construct, position and
// spelling, each case one document, which the harness's case oracles compare
// with its neighbours. Record holds what each case oracle is known to find at
// the core cases, each finding with the reason it stands. It is test/tooling
// infrastructure, not part of the compile pipeline: the harness reaches the
// compiler through the engine alone.
package openapicases
