// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

// Plural is plural, for the external tests.
var Plural = plural

// TemplateFault is templateFault, for the external tests.
var TemplateFault = templateFault

// ValidFormat is validFormat, for the external tests.
var ValidFormat = validFormat

// Objects returns the table's objects in declaration order, for the tests
// that hold every row to the table's own rules. The engine asks the table by
// kind alone.
func (s Spec) Objects() []Object { return s.objects }
