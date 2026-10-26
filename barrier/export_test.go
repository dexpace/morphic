package barrier

// MaxText exposes the bound on the text of a panic value in a message.
const MaxText = maxText

// ScanLimit exposes how much of a panic value's text is read.
const ScanLimit = scanLimit

// MaxStack exposes the bound on the stack a PanicError keeps.
const MaxStack = maxStack

// NoText exposes the text a panic value with none is given.
const NoText = noText

// ElidedMarker exposes the marker that ends a message the bound cut.
const ElidedMarker = elidedMarker

// PanicWithNoCallerFrame returns what Catch reports for a panic raised on a
// goroutine whose only frames are this package's and the runtime's, so the
// panic has no site to name.
func PanicWithNoCallerFrame() error {
	caught := make(chan error)
	go func() { caught <- Catch(func() { panic("bare") }) }()
	return <-caught
}
