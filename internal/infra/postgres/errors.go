// Package postgres provides pgx v5 implementations of the domain repository interfaces.
// Domain-specific sentinel errors (ErrNotFound) are defined in their respective domain packages
// (application.ErrNotFound, endpoint.ErrNotFound, admin.ErrNotFound) so that the service layer
// can check for them without importing infra packages (ADR-0015).
package postgres
