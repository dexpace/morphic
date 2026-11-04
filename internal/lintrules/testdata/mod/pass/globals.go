package pass

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"slices"
	_ "unsafe" // for go:linkname

	"github.com/dexpace/morphic/internal/lintrules/testdata/mod/shared"
)

type tally struct{ n int }

func (t *tally) Add()         { t.n++ }
func (t tally) Get() int      { return t.n }
func (t *tally) Self() *tally { return t }
func (t *tally) init()        { counter = 3 } // want "globalwrites: assigns the package-level variable counter; keep the state in a local or an argument"

// ptrAlias spells *tally, and a method declared on it takes the pointer.
type ptrAlias = *tally

func (t ptrAlias) Peek() int { return t.n }

// registry is a named map, and stack a named slice, whose methods write
// through the receiver.
type (
	registry map[string]int
	stack    []int
)

func (r registry) Set(k string) { r[k] = 1 }
func (r registry) Size() int    { return len(r) }
func (s *stack) Push(v int)     { *s = append(*s, v) }

// column is a named slice with a value receiver.
type column []int

func (c column) Set() { c[0] = 1 }

type holder struct{ tally }

type holderPtr struct{ *tally }

// opaque has no body, as a function written in assembly has, which a linkname
// lets the compiler accept.
//
//go:linkname opaque example.com/elsewhere.opaque
func opaque()

var (
	counter, total, subtotal int
	names                    = map[string]int{}
	lists                    = map[string][]int{}
	items                    []int
	rows                     [][]int
	pending                  [2]int
	ledger                   tally
	boxed                    holder
	boxedPtr                 = holderPtr{&tally{}}
	cursor                   = &tally{}
	shelf                    = &holder{}
	tallies                  []tally
	byPtr                    map[*tally]int
	queue                    chan []int
	store                    = registry{}
	slab                     stack
	col                      column
	captured                 = map[string]int{}
	capturedHook             func()
	storedInit               func()
	storedAlias              func()
	storedDecl               = ledger.Add // want "globalwrites: calls a method that can write the package-level variable ledger; keep the state in a local or an argument"
	readDecl                 = ledger.Get
	calledDecl               = store.Size()
	grid                     *[2][]int
	feed                     func(yield func([]int) bool)
	matcher                  = regexp.MustCompile(`x`)
	errNone                  = errors.New("none")
)

var viaCall = ledger.Self().n // want "globalwrites: calls a method that can write the package-level variable ledger; keep the state in a local or an argument"

// chained writes from a literal behind the method it calls.
var chained = func() tally {
	counter++ // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
	return tally{}
}().Get()

// hook and hooks write from function literals in their initializers, which can
// run after initialization, as init's own statements do not.
var hook = func() {
	counter++ // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
}

var hooks = []func(){func() { func() { counter-- }() }} // want "globalwrites: decrements the package-level variable counter; keep the state in a local or an argument"

func init() {
	counter = 1
	names["a"] = 1
	counter++
}

func init() {
	m := captured
	capturedHook = func() {
		m["a"]++ // want "globalwrites: writes an element or field of the package-level variable captured through its alias m; keep the state in a local or an argument"
	}
	m["b"] = 1
	q := cursor
	storedAlias = q.Add     // want "globalwrites: calls a method that can write the package-level variable cursor through its alias q; keep the state in a local or an argument"
	storedInit = ledger.Add // want "globalwrites: calls a method that can write the package-level variable ledger; keep the state in a local or an argument"
	ledger.Add()
	func() { counter-- }() // want "globalwrites: decrements the package-level variable counter; keep the state in a local or an argument"
	hook = func() {
		counter++ // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
	}
}

// Assigns writes each variable by assignment.
func Assigns() {
	counter = 1             // want "globalwrites: assigns the package-level variable counter; keep the state in a local or an argument"
	total, subtotal = 1, 2  // want "globalwrites: assigns the package-level variable total; keep the state in a local or an argument" "globalwrites: assigns the package-level variable subtotal; keep the state in a local or an argument"
	for counter = range 3 { // want "globalwrites: assigns the package-level variable counter; keep the state in a local or an argument"
	}
	for _, counter = range items { // want "globalwrites: assigns the package-level variable counter; keep the state in a local or an argument"
	}
	for names["k"] = range items { // want "globalwrites: writes an element or field of the package-level variable names; keep the state in a local or an argument"
	}
	for k, v := range lists {
		_, _ = k, v
	}
}

// Compounds writes each variable by compound assignment, ++ and --.
func Compounds() {
	counter += 2  // want "globalwrites: compound-assigns the package-level variable counter; keep the state in a local or an argument"
	counter <<= 1 // want "globalwrites: compound-assigns the package-level variable counter; keep the state in a local or an argument"
	counter++     // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
	counter--     // want "globalwrites: decrements the package-level variable counter; keep the state in a local or an argument"
}

// Elements writes what a variable holds, by every path to it.
func Elements() {
	names["a"] = 1        // want "globalwrites: writes an element or field of the package-level variable names; keep the state in a local or an argument"
	names["a"]++          // want "globalwrites: writes an element or field of the package-level variable names; keep the state in a local or an argument"
	names["a"] += 1       // want "globalwrites: writes an element or field of the package-level variable names; keep the state in a local or an argument"
	items[0] = 1          // want "globalwrites: writes an element or field of the package-level variable items; keep the state in a local or an argument"
	pending[1] = 3        // want "globalwrites: writes an element or field of the package-level variable pending; keep the state in a local or an argument"
	ledger.n = 2          // want "globalwrites: writes an element or field of the package-level variable ledger; keep the state in a local or an argument"
	cursor.n = 3          // want "globalwrites: writes an element or field of the package-level variable cursor; keep the state in a local or an argument"
	*cursor = tally{}     // want "globalwrites: writes an element or field of the package-level variable cursor; keep the state in a local or an argument"
	lists["a"][0] = 1     // want "globalwrites: writes an element or field of the package-level variable lists; keep the state in a local or an argument"
	boxed.tally.n = 4     // want "globalwrites: writes an element or field of the package-level variable boxed; keep the state in a local or an argument"
	(*cursor).n = 5       // want "globalwrites: writes an element or field of the package-level variable cursor; keep the state in a local or an argument"
	shared.Count++        // want "globalwrites: increments the package-level variable shared.Count; keep the state in a local or an argument"
	shared.Table["a"] = 1 // want "globalwrites: writes an element or field of the package-level variable shared.Table; keep the state in a local or an argument"
}

// Addresses takes the address of a variable and of what it holds.
func Addresses() {
	_ = &counter   // want "globalwrites: takes the address of the package-level variable counter; keep the state in a local or an argument"
	_ = &ledger.n  // want "globalwrites: takes the address of the package-level variable ledger; keep the state in a local or an argument"
	_ = &items[0]  // want "globalwrites: takes the address of the package-level variable items; keep the state in a local or an argument"
	_ = &cursor.n  // want "globalwrites: takes the address of the package-level variable cursor; keep the state in a local or an argument"
	_ = &(counter) // want "globalwrites: takes the address of the package-level variable counter; keep the state in a local or an argument"
}

// Methods calls methods that can write, and not those that cannot.
func Methods() {
	ledger.Add()      // want "globalwrites: calls a method that can write the package-level variable ledger; keep the state in a local or an argument"
	add := ledger.Add // want "globalwrites: calls a method that can write the package-level variable ledger; keep the state in a local or an argument"
	boxed.Add()       // want "globalwrites: calls a method that can write the package-level variable boxed; keep the state in a local or an argument"
	shelf.Add()       // want "globalwrites: calls a method that can write the package-level variable shelf; keep the state in a local or an argument"
	shelf.tally.Add() // want "globalwrites: calls a method that can write the package-level variable shelf; keep the state in a local or an argument"
	tallies[0].Add()  // want "globalwrites: calls a method that can write the package-level variable tallies; keep the state in a local or an argument"
	_ = add
	ledger.Peek()  // want "globalwrites: calls a method that can write the package-level variable ledger; keep the state in a local or an argument"
	boxedPtr.Add() // want "globalwrites: calls a method that can write the package-level variable boxedPtr; keep the state in a local or an argument"
	cursor.Add()   // want "globalwrites: calls a method that can write the package-level variable cursor; keep the state in a local or an argument"
	store.Set("a") // want "globalwrites: calls a method that can write the package-level variable store; keep the state in a local or an argument"
	col.Set()      // want "globalwrites: calls a method that can write the package-level variable col; keep the state in a local or an argument"
	_ = ledger.Get()
	_ = tally.Get(ledger)
	_ = (*tally).Add
	_ = matcher.MatchString("x")
	_ = base64.StdEncoding.EncodeToString(nil)
}

// Builtins writes through delete, clear, copy and append.
func Builtins() {
	delete(names, "a")       // want "globalwrites: writes through delete to the package-level variable names; keep the state in a local or an argument"
	clear(names)             // want "globalwrites: writes through clear to the package-level variable names; keep the state in a local or an argument"
	copy(items, []int{1})    // want "globalwrites: writes through copy to the package-level variable items; keep the state in a local or an argument"
	_ = append(items[:1], 2) // want "globalwrites: writes through append to the package-level variable items; keep the state in a local or an argument"
	_ = append(items, 3)     // want "globalwrites: writes through append to the package-level variable items; keep the state in a local or an argument"
	items = append(items, 1) // want "globalwrites: assigns the package-level variable items; keep the state in a local or an argument" "globalwrites: writes through append to the package-level variable items; keep the state in a local or an argument"
	(delete)(names, "b")     // want "globalwrites: writes through delete to the package-level variable names; keep the state in a local or an argument"
	_ = append([]int(nil), items...)
	_ = append(slices.Clone(items), 1)
	copy([]int{}, items)
}

// Aliases writes a variable through the locals that hold it.
func Aliases(k string) {
	m := names
	_ = &m         // want "globalwrites: takes the address of the package-level variable names through its alias m; keep the state in a local or an argument"
	m["a"] = 1     // want "globalwrites: writes an element or field of the package-level variable names through its alias m; keep the state in a local or an argument"
	delete(m, "a") // want "globalwrites: writes through delete to the package-level variable names through its alias m; keep the state in a local or an argument"
	n := m
	n["b"]++ // want "globalwrites: writes an element or field of the package-level variable names through its alias n; keep the state in a local or an argument"
	for _, row := range lists {
		row[0] = 1 // want "globalwrites: writes an element or field of the package-level variable lists through its alias row; keep the state in a local or an argument"
	}
	var p *tally
	p = cursor
	p.n = 1      // want "globalwrites: writes an element or field of the package-level variable cursor through its alias p; keep the state in a local or an argument"
	q := &ledger // want "globalwrites: takes the address of the package-level variable ledger; keep the state in a local or an argument"
	q.n = 1      // want "globalwrites: writes an element or field of the package-level variable ledger through its alias q; keep the state in a local or an argument"
	r := lists["a"]
	r[0] = 2 // want "globalwrites: writes an element or field of the package-level variable lists through its alias r; keep the state in a local or an argument"
	t, ok := lists["a"]
	t[0] = 1 // want "globalwrites: writes an element or field of the package-level variable lists through its alias t; keep the state in a local or an argument"
	var v = items
	v[0] = 1 // want "globalwrites: writes an element or field of the package-level variable items through its alias v; keep the state in a local or an argument"
	for _, row := range grid {
		row[0] = 1 // want "globalwrites: writes an element or field of the package-level variable grid through its alias row; keep the state in a local or an argument"
	}
	m = nil
	l := slab
	l.Push(1) // want "globalwrites: calls a method that can write the package-level variable slab through its alias l; keep the state in a local or an argument"
	for p := range byPtr {
		p.n = 1 // want "globalwrites: writes an element or field of the package-level variable byPtr through its alias p; keep the state in a local or an argument"
	}
	x, y := names, items
	x["b"] = 3 // want "globalwrites: writes an element or field of the package-level variable names through its alias x; keep the state in a local or an argument"
	y[0] = 2   // want "globalwrites: writes an element or field of the package-level variable items through its alias y; keep the state in a local or an argument"
	_, _ = ok, k
	for _, row := range rows {
		row[0] = 1 // want "globalwrites: writes an element or field of the package-level variable rows through its alias row; keep the state in a local or an argument"
	}
}

// Chain writes through an alias that is bound before its source is.
func Chain(k int) {
	var first, second []int
	second = first
	first = items
	second[k] = 1 // want "globalwrites: writes an element or field of the package-level variable items through its alias second; keep the state in a local or an argument"

	var m1 map[string]int
	(m1) = names
	m1["a"] = 1 // want "globalwrites: writes an element or field of the package-level variable names through its alias m1; keep the state in a local or an argument"

	var both = names
	both = shared.Table
	both["a"] = 1 // want "globalwrites: writes an element or field of the package-level variable names through its alias both; keep the state in a local or an argument"
}

// Literal writes from a function literal.
func Literal() func() {
	return func() {
		counter++ // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
	}
}

// Reads reads every variable, which draws nothing.
func Reads(k string) error {
	_ = names["a"]
	v, ok := names["a"]
	for range names {
	}
	for _, row := range lists {
		_ = row[0]
	}
	for part := range feed {
		part[0] = 1
	}
	_ = len(items) + cap(items) - counter
	_ = -counter
	_ = slices.Contains(items, 1)
	_ = ledger.n
	_, _, _ = v, ok, k
	if errors.Is(errNone, errNone) {
		return errNone
	}
	fmt.Println(counter, total, subtotal, pending)
	return nil
}

// Copies writes locals that look like writes to a variable.
func Copies() {
	c := ledger
	c.n = 5
	arr := pending
	arr[0] = 1
	f := hook
	f()
	n := counter
	n++
	s := fmt.Sprint(names)
	_ = s
	m := names
	m = map[string]int{}
	m = nil
	_ = m
	b := <-queue
	b[0] = 1
}

// Shadows writes locals named like variables.
func Shadows(items []int) {
	counter := 0
	counter++
	names := map[string]int{}
	names["a"] = 1
	items[0] = 1
}
