// Package auth lowers what a document says about authentication: the security
// schemes it declares, and the requirements that name them.
//
// It is a package of its own because both halves of the compiler reach it. The
// compiler's run lowers the schemes once, before anything references them; the
// service and operation walks lower requirements against the result. The
// requirements cannot sit in the root, which those walks may not import, and
// the schemes are the document's, no part of lowering an operation.
package auth
