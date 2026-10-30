package ir

import "iter"

// PathLog keeps the paths a walk reaches values by, in the order recorded,
// without spelling any. Each is kept as the steps it shares with the path
// recorded before it and the steps after those, so recording one copies the
// steps that differ and allocates nothing once the log has grown, where keeping
// a path's String costs a string. A path is spelled only when [PathLog.All]
// replays it and its String is asked for. The zero PathLog is ready to record
// into; one is not safe for concurrent use.
type PathLog struct {
	// last holds the steps of the path recorded last, whole.
	last []pathStep
	// tails holds each path's steps past the ones it shares with the path
	// recorded before it, back to back.
	tails   []pathStep
	entries []loggedPath
}

// loggedPath is one path a PathLog holds: the walk's root, how many steps it
// shares with the path recorded before it, and where its own steps end in
// tails. A step is shared whatever root it was reached from, since a path
// spells its root and then each step.
type loggedPath struct {
	root   string
	shared int
	end    int
}

// Record keeps at, the path to the value a visitor is being shown, at the next
// position of the log: the position Len reports before the call. The zero
// WalkPath is kept as the empty path it renders.
func (l *PathLog) Record(at WalkPath) {
	var root string
	var steps []pathStep
	if at.walk != nil {
		root, steps = at.walk.root, at.walk.steps[:at.depth]
	}
	shared := sharedSteps(l.last, steps)
	l.tails = appendGrowing(l.tails, steps[shared:]...)
	l.entries = appendGrowing(l.entries, loggedPath{root: root, shared: shared, end: len(l.tails)})
	l.last = append(l.last[:shared], steps[shared:]...)
}

// Len returns how many paths the log holds.
func (l *PathLog) Len() int {
	return len(l.entries)
}

// All replays the log: it yields each path in the order recorded, with its
// position, as a WalkPath that is valid until the next is yielded, so a caller
// spells only the paths it keeps. The log's own storage is not copied, but
// replaying a path costs a copy of its steps.
func (l *PathLog) All() iter.Seq2[int, WalkPath] {
	return func(yield func(int, WalkPath) bool) {
		replay := &valueWalk{}
		start := 0
		for i, e := range l.entries {
			replay.root = e.root
			replay.steps = append(replay.steps[:e.shared], l.tails[start:e.end]...)
			start = e.end
			if !yield(i, WalkPath{walk: replay, depth: len(replay.steps)}) {
				return
			}
		}
	}
}

// sharedSteps returns how many steps a and b open with alike.
func sharedSteps(a, b []pathStep) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// appendGrowing appends vs to s, growing s's capacity five halves whenever it
// fills. append grows a large slice by a quarter at a time, so a slice built
// one entry at a time is copied several times over, where this copies each
// entry less than once on average. slices.Grow grows about as far, but a race
// build makes two allocations for each of its growths.
func appendGrowing[T any](s []T, vs ...T) []T {
	if need := len(s) + len(vs); need > cap(s) {
		grown := make([]T, len(s), max(need, cap(s)*5/2))
		copy(grown, s)
		s = grown
	}
	return append(s, vs...)
}
