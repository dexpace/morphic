// Package naming names every node the OpenAPI compiler builds as a function
// of the documents and the node's coordinate (GitHub #981), so which
// lowering reaches a coordinate first cannot change what it is called.
//
// A name is an owner's, then the structure beneath it. The owner is the object
// the grammar reads a schema position as belonging to (bind.Table.Kinds): a
// component schema, a request body, a response, a parameter or a header. Each
// keyword beneath it composes the name of the position it leads to, and a
// composition branch holding a $ref takes the name of the position it lands on.
package naming
