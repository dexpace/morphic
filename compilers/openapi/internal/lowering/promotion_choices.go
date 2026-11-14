// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

// TargetParamChoices fills ir.Operation.ParamChoices from a key on the
// operation naming alternatives among its parameters
// (docs/compilers/openapi.md#mutually-exclusive-parameters).
const TargetParamChoices ExtensionTarget = "operation.paramChoices"
