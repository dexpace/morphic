// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package paramchoice reads what the promotion policy maps an operation's key
// to, the choices among its parameters, once the document is whole
// (docs/compilers/openapi.md#mutually-exclusive-parameters): each names
// alternative sets of the operation's parameters by name, resolved as a
// link's parameters key is. It writes Operation.ParamChoices and nothing
// else, so the parameters and every reference to one stay as they were.
package paramchoice
