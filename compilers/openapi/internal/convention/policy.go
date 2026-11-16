// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import "github.com/dexpace/morphic/compilers/openapi/internal/lowering"

// Policy holds the policies of what a document's names imply, as the
// compiler's Options resolve them: one for each reading Apply makes.
type Policy struct {
	Idempotency lowering.IdempotencyInference
	Secret      lowering.SecretInference
	HTTPDate    lowering.HTTPDateInference
}
