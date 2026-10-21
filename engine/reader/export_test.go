package reader

// ErrNotRegular exposes the error a read of a directory, FIFO, device or
// socket fails with, so the external tests assert on it rather than on its
// wording.
var ErrNotRegular = errNotRegular
