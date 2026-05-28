//go:build !windows

package dpapi

// Protect always returns ErrUnsupportedOS on non-Windows platforms.
// The gateway boot path on Linux must rely on systemd-creds (follow-up ADR)
// or refuse to start when SECRET_PROVIDER=db is set.
func Protect(_ []byte) ([]byte, error) {
	return nil, ErrUnsupportedOS
}

// Unprotect always returns ErrUnsupportedOS on non-Windows platforms.
func Unprotect(_ []byte) ([]byte, error) {
	return nil, ErrUnsupportedOS
}
