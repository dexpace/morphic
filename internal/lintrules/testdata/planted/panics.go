package planted

// plantRecover recovers a panic by hand, which no package but barrier may.
func plantRecover() {
	defer func() { _ = recover() }() // want "use of `recover` forbidden because \"recover a panic through barrier.Catch\" (forbidigo)"
}
