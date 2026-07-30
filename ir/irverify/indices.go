package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var (
	serviceType   = reflect.TypeFor[ir.Service]()
	channelType   = reflect.TypeFor[ir.Channel]()
	operationType = reflect.TypeFor[ir.Operation]()
)

// checkIndices asserts every reference the IR carries as an integer index into a
// slice addresses a declared entry: Service.Servers and Channel.Servers into
// Document.Servers, and HTTPBinding.SuccessStatus's keys into the owning
// operation's Responses.
//
// Nothing in an int's Go type marks it as a reference, so collectRefs cannot
// reach these the way it reaches typed IDs — the carriers are named here
// instead, and a new integer-index reference has to be named here too.
// Provenance.Source, the third such index, has its own check in provenance.go.
//
// Naming a carrier means reaching its fields by name, which the Go compiler
// cannot check: renaming or retyping ir.Service.Servers leaves FieldByName
// returning the zero reflect.Value, and the Len() below panics on it. The
// guarantee this package makes — that Verify never crashes on a malformed
// document — is unaffected, since no input can rename a field, but the coupling
// is real and is guarded the way this package guards its other hand-written
// couplings: indexCarrierFields (indices_test.go) fails the moment one of these
// names or shapes drifts, the same role TestStringTypes_AreAllClassified plays
// for refKindByType.
func checkIndices(doc *ir.Document) []Violation {
	declared := len(doc.Servers)
	var vs []Violation
	walkValues(doc, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct {
			return true
		}
		switch v.Type() {
		case serviceType, channelType:
			vs = appendServerIndexViolations(vs, v.FieldByName("Servers"), declared, path)
		case operationType:
			vs = appendResponseIndexViolations(vs, v, path)
		default:
			// Every other struct carries no index reference of its own.
		}
		return true // a service still owns the operations nested below it
	})
	return vs
}

// appendServerIndexViolations appends to vs a violation per entry of a Servers
// field that addresses none of the declared servers.
func appendServerIndexViolations(vs []Violation, servers reflect.Value, declared int, path string) []Violation {
	for i := range servers.Len() {
		index := int(servers.Index(i).Int())
		if index >= 0 && index < declared {
			continue
		}
		vs = append(vs, Violation{
			Code: "ir/server-index-out-of-range",
			Message: "server index " + strconv.Itoa(index) + " addresses none of the " +
				strconv.Itoa(declared) + " declared servers",
			Path: path + ".Servers[" + strconv.Itoa(i) + "]",
		})
	}
	return vs
}

// appendResponseIndexViolations appends to vs a violation per SuccessStatus key
// on one operation's HTTP bindings that addresses no response it declares.
func appendResponseIndexViolations(vs []Violation, op reflect.Value, path string) []Violation {
	declared := op.FieldByName("Responses").Len()
	bindings := op.FieldByName("Bindings").FieldByName("HTTP")
	for i := range bindings.Len() {
		at := path + ".Bindings.HTTP[" + strconv.Itoa(i) + "]"
		status := bindings.Index(i).FieldByName("SuccessStatus")
		vs = appendSuccessStatusViolations(vs, status, declared, at)
	}
	return vs
}

// appendSuccessStatusViolations appends to vs a violation per key of one
// SuccessStatus map that addresses none of the declared responses. Keys need no
// sorting first: every violation carries its own key in Path, and Verify orders
// the whole result by (Code, Path) before returning it.
func appendSuccessStatusViolations(vs []Violation, status reflect.Value, declared int, path string) []Violation {
	iter := status.MapRange()
	for iter.Next() {
		index := int(iter.Key().Int())
		if index >= 0 && index < declared {
			continue
		}
		vs = append(vs, Violation{
			Code: "ir/response-index-out-of-range",
			Message: "response index " + strconv.Itoa(index) + " addresses none of the " +
				strconv.Itoa(declared) + " declared responses",
			Path: path + ".SuccessStatus[" + strconv.Itoa(index) + "]",
		})
	}
	return vs
}
