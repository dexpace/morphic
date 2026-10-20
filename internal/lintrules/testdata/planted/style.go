package planted

import (
	"context"
	"sync"
)

// userId spells an initialism otherwise than Go does.
var userId int // want "var-naming: var userId should be userID (revive)"

// anything is an empty interface spelled the old way.
var anything interface{} // want "use-any: since Go 1.18 'interface{}' can be replaced by 'any' (revive)"

// receiver is named one way by one method and another way by the next.
type receiver struct{}

// M names its receiver r.
func (r *receiver) M() {}

// N names it s.
func (s *receiver) N() {} // want "receiver-naming: receiver name s should be consistent with previous receiver name r for receiver (revive)"

// unexported is a type no other package can name.
type unexported struct{}

// Exported hands out a type its callers cannot name.
func Exported() *unexported { return nil } // want "unexported-return: exported func Exported returns unexported type *planted.unexported, which can be annoying to use (revive)"

func plantContextLast(n int, ctx context.Context) int { return n } // want "context-as-argument: context.Context should be the first parameter of a function (revive)"

func plantErrorFirst() (error, int) { return nil, 0 } // want "error-return: error should be the last type when returning multiple items (revive)"

func plantRecursion(n int) int { return plantRecursion(n) } // want "unconditional-recursion: unconditional recursive call (revive)"

func plantWaitGroup(wg sync.WaitGroup) {} // want "waitgroup-by-value: sync.WaitGroup passed by value, the function will get a copy of the original one (revive)"

func plantRace() (err error) {
	done := make(chan struct{})
	go func() {
		err = nil // want "datarace: potential datarace: return value err is captured (by-reference) in goroutine (revive)"
		close(done)
	}()
	<-done
	return err
}

// plantBranches draws the revive rules about if and else.
func plantBranches(err error, ok bool, xs []bool) int {
	n := 0
	if err != nil {
		return 1
	} else { // want "indent-error-flow: if block ends with a return statement, so drop this else and outdent its block (revive)"
		n++
	}
	for _, x := range xs {
		if x {
			continue
		} else { // want "superfluous-else: if block ends with a continue statement, so drop this else and outdent its block (revive)"
			n++
		}
	}
	if ok { // want "early-return: if c { ... } else { ... return } can be simplified to if !c { ... return } ... (revive)"
		n++
	} else {
		return n
	}
	if n > 1 { // want "identical-branches: both branches of the if are identical (revive)"
		n++
	} else {
		n++
	}
	_, _ = userId, anything
	return n
}
