// Package auth lowers what a document says about authentication: the security
// schemes it declares, and the requirements that name them.
//
// It is a package of its own because both halves of the compiler reach it and
// neither may reach the other. The document walk lowers the schemes once, before
// anything references them; the service and operation walks lower requirements
// against the result. Putting either half's lowering with its caller would make
// the other import it.
package auth
