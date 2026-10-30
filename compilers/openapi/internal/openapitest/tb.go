package openapitest

// TB is the subset of *testing.T these helpers need.
//
// It is an interface rather than *testing.T so this package's own tests can
// drive the failure branches with a recording stub, the way ir/irtest does:
// a helper that aborts a real test cannot have its abort path covered.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	FailNow()
	Fatalf(format string, args ...any)
}
