package ports

import "errors"

// ErrNotFound is returned by a repository when the requested aggregate does
// not exist.
var ErrNotFound = errors.New("not found")
