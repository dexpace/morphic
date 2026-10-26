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

// checkIndices asserts every integer-index reference the IR carries addresses a
// declared entry: Service.Servers and Channel.Servers into Document.Servers,
// and HTTPBinding.SuccessStatus's keys into the owning operation's Responses.
//
// Nothing in an int's type marks it as a reference, so checkReferentialIntegrity
// cannot reach these: the carriers are named here, and a new one must be too.
// Provenance.Source is checked in provenance.go.
//
// Reaching fields by name is unchecked by the compiler: a renamed
// ir.Service.Servers would make the Len() below panic. indexCarrierFields fails
// when a name or shape drifts, and integerFields fails when the IR grows an
// integer field nobody has classified.
func checkIndices(doc *ir.Document) walkCheck {
	declared := len(doc.Servers)
	var vs []Violation
	var reader entryReader
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Kind() != reflect.Struct {
				return true
			}
			switch v.Type() {
			case serviceType, channelType:
				vs = appendServerIndexViolations(vs, v.FieldByName("Servers"), declared, at)
			case operationType:
				vs = appendResponseIndexViolations(vs, &reader, v, at)
			default:
				// Every other struct carries no index reference of its own.
			}
			return true // a service still owns the operations nested below it
		},
		report: reporting(&vs),
	}
}

// appendServerIndexViolations appends to vs a violation per entry of a Servers
// field that addresses none of the declared servers.
func appendServerIndexViolations(vs []Violation, servers reflect.Value, declared int, at ir.WalkPath) []Violation {
	for i := range servers.Len() {
		index := int(servers.Index(i).Int())
		if index >= 0 && index < declared {
			continue
		}
		vs = append(vs, Violation{
			Code: "ir/server-index-out-of-range",
			Message: "server index " + strconv.Itoa(index) + " addresses none of the " +
				strconv.Itoa(declared) + " declared servers",
			Path: at.String() + ".Servers[" + strconv.Itoa(i) + "]",
		})
	}
	return vs
}

// appendResponseIndexViolations appends to vs a violation per SuccessStatus key
// on one operation's HTTP bindings that addresses no response it declares; r
// reads each SuccessStatus map. Keys need no sorting first: every violation
// carries its own key in Path, and Verify orders the whole result by (Code,
// Path) before returning it.
func appendResponseIndexViolations(vs []Violation, r *entryReader, op reflect.Value, at ir.WalkPath) []Violation {
	declared := op.FieldByName("Responses").Len()
	bindings := op.FieldByName("Bindings").FieldByName("HTTP")
	for i := range bindings.Len() {
		status := bindings.Index(i).FieldByName("SuccessStatus")
		held := r.heldFor(status)
		for iter := status.MapRange(); iter.Next(); {
			key, _ := held.read(iter)
			if index := int(key.Int()); index < 0 || index >= declared {
				vs = append(vs, responseIndexViolation(index, declared, at, i))
			}
		}
	}
	return vs
}

// responseIndexViolation reports the SuccessStatus key index, on the binding-th
// HTTP binding of the operation at op, as addressing none of its declared
// responses.
func responseIndexViolation(index, declared int, op ir.WalkPath, binding int) Violation {
	return Violation{
		Code: "ir/response-index-out-of-range",
		Message: "response index " + strconv.Itoa(index) + " addresses none of the " +
			strconv.Itoa(declared) + " declared responses",
		Path: op.String() + ".Bindings.HTTP[" + strconv.Itoa(binding) + "].SuccessStatus[" + strconv.Itoa(index) + "]",
	}
}
