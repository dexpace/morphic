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

// Tally takes a map as an argument, which a method of any receiver may write.
func (t *tally) Tally(m map[string]int) { t.n += len(m) }

// Bind keeps its receiver in the closure it returns.
func (t *tally) Bind() func() { return func() { t.n++ } }

// Gen and Two are generic methods, which a call instantiates in its Fun.
func (t *tally) Gen[X any]()    { t.n++ }
func (t *tally) Two[X, Y any]() { t.n++ }

func (t *tally) init() { counter = 3 } // want "globalwrites: assigns the package-level variable counter; keep the state in a local or an argument"

// initTables is named like init, and is not it.
func initTables() { counter = 2 } // want "globalwrites: assigns the package-level variable counter; keep the state in a local or an argument"

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

// namedRows and namedGrid range as the slice and the pointer to an array they name.
type (
	namedRows [][]int
	namedGrid *[2][]int
)

// column is a named slice with a value receiver.
type column []int

func (c column) Set() { c[0] = 1 }

// Named array, integer, channel and function types, each with a value receiver
// that only reads.
type (
	pair  [2]int
	level int
	pipe  chan int
	step  func()
)

func (p pair) First() int { return p[0] }
func (l level) Next() int { return int(l) + 1 }
func (p pipe) Len() int   { return len(p) }
func (s step) Run()       { s() }

// Encoding shares its name with a receiver the rule lists, from another package.
type Encoding struct{ n int }

func (e *Encoding) Set() { e.n++ }

type holder struct{ tally }

type holderPtr struct{ *tally }

// opaque has no body, as a function written in assembly has, which a linkname
// lets the compiler accept.
//
//go:linkname opaque example.com/elsewhere.opaque
func opaque()

// opaqueMap has no body and a named result.
//
//go:linkname opaqueMap example.com/elsewhere.opaqueMap
func opaqueMap() (m map[string]int)

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
	storedGen                func()
	storedDecl               = ledger.Add // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"
	readDecl                 = ledger.Get
	calledDecl               = store.Size()
	grid                     *[2][]int
	byName                   namedRows
	byNamedPtr               namedGrid
	feed                     func(yield func([]int) bool)
	matcher                  = regexp.MustCompile(`x`)
	errNone                  = errors.New("none")
	encoded                  Encoding
	couple                   pair
	depth                    level
	piped                    pipe
	stepper                  step
)

var viaCall = ledger.Self().n // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"

// chained writes from a literal behind the method it calls.
var chained = func() tally {
	counter++ // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
	return tally{}
}().Get()

// chainedField writes from a literal behind the field it selects.
var chainedField = func() tally { counter++; return tally{} }().n // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"

// hook and hooks write from function literals in their initializers, which can
// run after initialization, as init's own statements, bar a go statement, do not.
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
	storedAlias = q.Add     // want "globalwrites: uses a method that can write the package-level variable cursor through its alias q; keep the state in a local or an argument"
	storedInit = ledger.Add // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"
	ledger.Add()
	(ledger.Add)()
	ledger.Gen[int]()
	ledger.Two[int, string]()
	(ledger.Gen[int])()
	storedGen = ledger.Gen[int]           // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"
	go ledger.Add()                       // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"
	go func(f func()) { f() }(ledger.Add) // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"
	defer ledger.Add()
	func() { counter-- }() // want "globalwrites: decrements the package-level variable counter; keep the state in a local or an argument"
	hook = func() {
		counter++ // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
	}
}

// Assigns writes variables by assignment and by a range clause's =.
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

// Compounds writes counter by compound assignment, ++ and --.
func Compounds() {
	counter += 2  // want "globalwrites: compound-assigns the package-level variable counter; keep the state in a local or an argument"
	counter <<= 1 // want "globalwrites: compound-assigns the package-level variable counter; keep the state in a local or an argument"
	counter++     // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
	counter--     // want "globalwrites: decrements the package-level variable counter; keep the state in a local or an argument"
}

// Elements writes what a variable holds, through an index, a field and a pointer.
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
	ledger.Add()      // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"
	add := ledger.Add // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"
	boxed.Add()       // want "globalwrites: uses a method that can write the package-level variable boxed; keep the state in a local or an argument"
	shelf.Add()       // want "globalwrites: uses a method that can write the package-level variable shelf; keep the state in a local or an argument"
	shelf.tally.Add() // want "globalwrites: uses a method that can write the package-level variable shelf; keep the state in a local or an argument"
	tallies[0].Add()  // want "globalwrites: uses a method that can write the package-level variable tallies; keep the state in a local or an argument"
	_ = add
	ledger.Peek()  // want "globalwrites: uses a method that can write the package-level variable ledger; keep the state in a local or an argument"
	boxedPtr.Add() // want "globalwrites: uses a method that can write the package-level variable boxedPtr; keep the state in a local or an argument"
	cursor.Add()   // want "globalwrites: uses a method that can write the package-level variable cursor; keep the state in a local or an argument"
	store.Set("a") // want "globalwrites: uses a method that can write the package-level variable store; keep the state in a local or an argument"
	col.Set()      // want "globalwrites: uses a method that can write the package-level variable col; keep the state in a local or an argument"
	_ = ledger.Get()
	_ = tally.Get(ledger)
	_ = (*tally).Add
	(*tally).Add(cursor)              // want "globalwrites: uses a method that can write the package-level variable cursor; keep the state in a local or an argument"
	(*tally).Gen[int](cursor)         // want "globalwrites: uses a method that can write the package-level variable cursor; keep the state in a local or an argument"
	(*tally).Two[int, string](cursor) // want "globalwrites: uses a method that can write the package-level variable cursor; keep the state in a local or an argument"
	((*tally).Add)(cursor)            // want "globalwrites: uses a method that can write the package-level variable cursor; keep the state in a local or an argument"
	via := cursor
	(*tally).Add(via)        // want "globalwrites: uses a method that can write the package-level variable cursor through its alias via; keep the state in a local or an argument"
	registry.Set(store, "a") // want "globalwrites: uses a method that can write the package-level variable store; keep the state in a local or an argument"
	_ = (*base64.Encoding).EncodeToString(base64.StdEncoding, nil)
	_ = matcher.MatchString("x") // want "globalwrites: uses a method that can write the package-level variable matcher; keep the state in a local or an argument"
	_ = base64.StdEncoding.EncodeToString(nil)
}

// Builtins writes through delete, clear, copy and append.
func Builtins() {
	delete(names, "a")        // want "globalwrites: writes through delete to the package-level variable names; keep the state in a local or an argument"
	clear(names)              // want "globalwrites: writes through clear to the package-level variable names; keep the state in a local or an argument"
	copy(items, []int{1})     // want "globalwrites: writes through copy to the package-level variable items; keep the state in a local or an argument"
	_ = append(items[:1], 2)  // want "globalwrites: writes through append to the package-level variable items; keep the state in a local or an argument"
	_ = append(items, 3)      // want "globalwrites: writes through append to the package-level variable items; keep the state in a local or an argument"
	items = append(items, 1)  // want "globalwrites: assigns the package-level variable items; keep the state in a local or an argument" "globalwrites: writes through append to the package-level variable items; keep the state in a local or an argument"
	copy(items[1:], []int{1}) // want "globalwrites: writes through copy to the package-level variable items; keep the state in a local or an argument"
	clear(rows[0])            // want "globalwrites: writes through clear to the package-level variable rows; keep the state in a local or an argument"
	(delete)(names, "b")      // want "globalwrites: writes through delete to the package-level variable names; keep the state in a local or an argument"
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
	for _, row := range byName {
		row[0] = 1 // want "globalwrites: writes an element or field of the package-level variable byName through its alias row; keep the state in a local or an argument"
	}
	for _, row := range byNamedPtr {
		row[0] = 1 // want "globalwrites: writes an element or field of the package-level variable byNamedPtr through its alias row; keep the state in a local or an argument"
	}
	m = nil
	l := slab
	l.Push(1) // want "globalwrites: uses a method that can write the package-level variable slab through its alias l; keep the state in a local or an argument"
	for p := range byPtr {
		p.n = 1 // want "globalwrites: writes an element or field of the package-level variable byPtr through its alias p; keep the state in a local or an argument"
	}
	x, y := names, items
	x["b"] = 3 // want "globalwrites: writes an element or field of the package-level variable names through its alias x; keep the state in a local or an argument"
	y[0] = 2   // want "globalwrites: writes an element or field of the package-level variable items through its alias y; keep the state in a local or an argument"
	var bound []int
	for _, bound = range rows {
		bound[0] = 1 // want "globalwrites: writes an element or field of the package-level variable rows through its alias bound; keep the state in a local or an argument"
	}
	for _, row := range sheets {
		row[0] = 1 // want "globalwrites: writes an element or field of the package-level variable sheets through its alias row; keep the state in a local or an argument"
	}
	var kept []int
	kept, ok = lists["b"]
	kept[0] = 1 // want "globalwrites: writes an element or field of the package-level variable lists through its alias kept; keep the state in a local or an argument"
	var spec, specOK = lists["c"]
	spec[0] = 1 // want "globalwrites: writes an element or field of the package-level variable lists through its alias spec; keep the state in a local or an argument"
	_ = specOK
	totals := names
	totals["c"] += 1 // want "globalwrites: writes an element or field of the package-level variable names through its alias totals; keep the state in a local or an argument"
	clear(totals)    // want "globalwrites: writes through clear to the package-level variable names through its alias totals; keep the state in a local or an argument"
	seq := items
	copy(seq, []int{1}) // want "globalwrites: writes through copy to the package-level variable items through its alias seq; keep the state in a local or an argument"
	_ = append(seq, 1)  // want "globalwrites: writes through append to the package-level variable items through its alias seq; keep the state in a local or an argument"
	_, _ = ok, k
	for _, row := range rows {
		row[0] = 1 // want "globalwrites: writes an element or field of the package-level variable rows through its alias row; keep the state in a local or an argument"
	}
}

// Chain writes through an alias that is bound before its source is, three
// hops from the variable and in the reverse of the order the passes walk.
func Chain(k int) {
	var first, second, third []int
	third = second
	second = first
	first = items
	third[k] = 1 // want "globalwrites: writes an element or field of the package-level variable items through its alias third; keep the state in a local or an argument"

	var m1 map[string]int
	(m1) = names
	m1["a"] = 1 // want "globalwrites: writes an element or field of the package-level variable names through its alias m1; keep the state in a local or an argument"

	var both = names
	both = shared.Table
	both["a"] = 1 // want "globalwrites: writes an element or field of the package-level variable names through its alias both; keep the state in a local or an argument"
}

// ChainAlone is a reverse chain and no other binding, so the fixed point needs a
// pass for each binding it has.
func ChainAlone(k int) {
	var first, second, third, fourth []int
	fourth = third
	third = second
	second = first
	first = items
	fourth[k] = 1 // want "globalwrites: writes an element or field of the package-level variable items through its alias fourth; keep the state in a local or an argument"
}

// NamedResult writes, after the return, through the named result it binds.
func NamedResult() (m map[string]int) {
	defer func() {
		m["a"]++ // want "globalwrites: writes an element or field of the package-level variable names through its alias m; keep the state in a local or an argument"
	}()
	return names
}

// NamedResults binds each of three results, one of a type that shares nothing.
func NamedResults() (n int, s, t []int) {
	defer func() {
		s[0] = n // want "globalwrites: writes an element or field of the package-level variable items through its alias s; keep the state in a local or an argument"
		t[0] = n // want "globalwrites: writes an element or field of the package-level variable rows through its alias t; keep the state in a local or an argument"
	}()
	return counter, items, rows[0]
}

// LiteralResult binds a literal's own named result.
func LiteralResult() func() map[string]int {
	return func() (r map[string]int) {
		defer func() {
			r["a"]++ // want "globalwrites: writes an element or field of the package-level variable names through its alias r; keep the state in a local or an argument"
		}()
		return names
	}
}

// ReturnsOnly returns a variable through a named result, which writes nothing.
func ReturnsOnly() (m map[string]int) { return names }

// OwnResult fills a result of its own, beside a literal returning a variable,
// whose return binds the literal's result and not this one.
func OwnResult() (m map[string]int) {
	_ = func() map[string]int { return names }
	m = map[string]int{}
	m["a"] = 1
	return
}

// PathResult returns the variable on one path and fills a map of its own on
// another, and is read without order: the result holds the variable throughout.
func PathResult(x bool) (m map[string]int) {
	if x {
		return names
	}
	m = map[string]int{}
	m["a"] = 1 // want "globalwrites: writes an element or field of the package-level variable names through its alias m; keep the state in a local or an argument"
	return m
}

// SeparateResult keeps the map of its own in a local, the remedy for PathResult.
func SeparateResult(x bool) (m map[string]int) {
	if x {
		return names
	}
	own := map[string]int{}
	own["a"] = 1
	return own
}

// Literal writes from a function literal.
func Literal() func() {
	return func() {
		counter++ // want "globalwrites: increments the package-level variable counter; keep the state in a local or an argument"
	}
}

// Reads reads variables of several kinds, which draws nothing.
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
	for part := range queue {
		part[0] = 1
	}
	_ = len(items) + cap(items) - counter
	_ = -counter
	_ = slices.Contains(items, 1)
	_ = ledger.n
	_, _, _ = v, ok, k
	_ = errNone.Error()
	own := &tally{}
	own.Tally(names)
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
	_ = &f
	q := queue
	_ = &q
	e := errNone
	_ = &e
	n := counter
	n++
	_ = &n
	s := fmt.Sprint(names)
	_ = s
	m := names
	m = map[string]int{}
	m = nil
	_ = m
	b := <-queue
	b[0] = 1
}

// ReadsNamedKinds calls the read-only value-receiver methods of a named array,
// integer, channel and function type on package-level variables of them.
func ReadsNamedKinds() int {
	stepper.Run()
	return couple.First() + depth.Next() + piped.Len()
}

// ShadowedBuiltin calls a parameter named like a builtin, which writes nothing.
func ShadowedBuiltin(clear func([]int)) {
	clear(items)
}

// Named calls a pointer method of a type named like a listed receiver, which
// draws as the listed one is another package's.
func Named() {
	encoded.Set() // want "globalwrites: uses a method that can write the package-level variable encoded; keep the state in a local or an argument"
}

// ShadowedImport writes through a local named like an import.
func ShadowedImport() {
	shared := cursor
	shared.n = 1 // want "globalwrites: writes an element or field of the package-level variable cursor through its alias shared; keep the state in a local or an argument"
}

// Shadows writes locals named like variables.
func Shadows(items []int) {
	counter := 0
	counter++
	names := map[string]int{}
	names["a"] = 1
	items[0] = 1
}

var (
	typed  any = map[string]int{}
	sheets [2][]int
	keeper = ledger.Bind()
	hoard  = stash{m: map[string]int{}}
)

// stash is a struct value holding a map, which its value receiver writes through.
type stash struct{ m map[string]int }

func (s stash) Set() { s.m["a"] = 1 }

// handed writes what it is given, and table returns a variable.
func handed(v []int) { v[0] = 1 }

func table() map[string]int { return names }

// OutOfReach spells what the rule states it cannot see, which draws nothing: a
// channel send or close, a conversion, a type assertion, a callee given a
// variable, a function's result, a method on a value holding a map, and an alias
// held in a literal, an element, a copied struct or array, or behind a pointer.
// keeper above is the method at package level that keeps its receiver.
func OutOfReach() {
	queue <- items
	close(queue)
	registry(names)["a"] = 1
	typed.(map[string]int)["a"] = 1
	held := []map[string]int{names}
	held[0]["a"] = 1
	own := make([]map[string]int, 1)
	own[0] = names
	own[0]["a"] = 1
	behind := new(map[string]int)
	*behind = names
	(*behind)["a"] = 1
	slices.Sort(items)
	handed(items)
	table()["a"] = 1
	hoard.Set()
	copied := hoard
	copied.m["a"] = 1
	gridCopy := sheets
	gridCopy[0][0] = 1
}
