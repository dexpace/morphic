package pass

import (
	"iter"
	"slices"
	"sort"
	"strings"

	lookalike "github.com/dexpace/morphic/internal/lintrules/testdata/mod/lookalike/ir"
	"github.com/dexpace/morphic/ir"
)

// operations is a slice of the service tree under another name.
type operations []ir.Operation

// swap writes the slice it is a method of.
func (o operations) swap(i, j int) {
	o[i], o[j] = o[j], o[i] // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
}

// slicesCalls builds and writes a tree slice through each function of package
// slices that does either.
func slicesCalls(irOperations []ir.Operation, groups []ir.OperationGroup, seq iter.Seq[ir.Operation]) {
	byID := func(a, b ir.Operation) int { return strings.Compare(string(a.ID), string(b.ID)) }
	same := func(a, b ir.Operation) bool { return a.ID == b.ID }
	_ = slices.Concat(irOperations, irOperations)           // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = slices.Grow(groups, 1)                              // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = slices.Repeat(irOperations, 2)                      // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = slices.SortedFunc(seq, byID)                        // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = slices.SortedStableFunc(seq, byID)                  // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = slices.AppendSeq(irOperations, seq)                 // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	_ = slices.CompactFunc(irOperations, same)              // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	_ = slices.Delete(irOperations, 0, 1)                   // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	_ = slices.Insert(groups, 0, ir.OperationGroup{})       // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	_ = slices.Replace(irOperations, 0, 1, irOperations[1]) // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	slices.Reverse(groups)                                  // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	slices.SortStableFunc(irOperations, byID)               // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	_ = slices.ContainsFunc(irOperations, func(operation ir.Operation) bool { return operation.ID == "" })
	_, _ = slices.BinarySearchFunc(irOperations, irOperations[0], byID)
}

// Tree builds and writes the service tree in each way the tree rule refuses,
// and reads it in each way it leaves alone.
func Tree(doc *ir.Document, operation ir.Operation, seq iter.Seq[ir.Operation]) *ir.Operation {
	svc := doc.Services[0]
	irOperations := svc.Groups[0].Operations

	_ = []ir.Operation{operation}                                                           // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = make([]ir.OperationGroup, 1)                                                        // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = operations{}                                                                        // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = slices.Clone(doc.Services)                                                          // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	_ = slices.Collect(seq)                                                                 // want "servicetreewrites: builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	svc.Groups[0].Docs.Summary = "changed"                                                  // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	irOperations[0] = operation                                                             // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	(irOperations[0].Params) = nil                                                          // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	(*irOperations[0].Request).Contents = nil                                               // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	irOperations[0].Bindings.HTTP[0].SuccessStatus[0]++                                     // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	irOperations = append(irOperations, operation)                                          // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	copy(irOperations, irOperations[1:])                                                    // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	clear(svc.Groups)                                                                       // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	irOperations = slices.DeleteFunc(irOperations, func(ir.Operation) bool { return true }) // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	slices.SortFunc(doc.Services, func(ir.Service, ir.Service) int { return 0 })            // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	sort.Slice(irOperations, func(int, int) bool { return false })                          // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	for i := range irOperations {
		irOperations[i].Tags = nil // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	}
	for _, irOperations[0] = range irOperations { // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	}
	for irOperations[0].Bindings.HTTP[0].SuccessStatus[0] = range irOperations { // want "servicetreewrites: writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	}
	_ = &doc.Services[0].Docs // want "servicetreewrites: takes the address of an element of the service tree outside ir and the compilers, through which it can write a document; read the element by value and edit it through an ir.Editor"
	keepAny(irOperations)     // want "servicetreewrites: instantiates a generic with a slice or an element of the service tree, whose writes this rule cannot read; keep service tree writes in ir and the compilers"
	keepAny(operation)        // want "servicetreewrites: instantiates a generic with a slice or an element of the service tree, whose writes this rule cannot read; keep service tree writes in ir and the compilers"
	operations(irOperations).swap(0, 1)

	_ = irOperations[0].Docs
	_ = len(doc.Services)
	_ = irOperations[1:]
	_ = slices.IndexFunc(irOperations, func(ir.Operation) bool { return true })
	_ = slices.Clip(irOperations)
	for _, o := range irOperations {
		o.Docs.Summary = "a copy of its own"
	}
	ids := make([]ir.OperationID, 0, len(irOperations))
	ids = append(ids, operation.ID)
	keepAny(ids)
	_ = slices.Contains(ids, operation.ID)
	_ = []lookalike.Operation{}
	built := &ir.Operation{ID: operation.ID}
	built.Docs.Summary = "built here"
	return &operation
}
