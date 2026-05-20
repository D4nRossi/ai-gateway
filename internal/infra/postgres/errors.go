package postgres

import "errors"

// ErrNotFound is returned by repository methods when the requested row does not exist.
// Callers can use errors.Is(err, ErrNotFound) to distinguish a missing record from
// other database errors and respond with 404 instead of 500.
var ErrNotFound = errors.New("not found")
