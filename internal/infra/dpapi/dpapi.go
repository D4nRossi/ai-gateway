// Package dpapi wraps the Windows Data Protection API (CryptProtectData /
// CryptUnprotectData) so the gateway can encrypt the bootstrap `.env` file at
// rest without an external KMS (ADR-0026).
//
// The package is OS-conditional: real implementation lives in dpapi_windows.go;
// non-Windows builds get a stub that returns ErrUnsupportedOS. This lets the
// codebase compile and unit-test on Linux/macOS while the operational path is
// Windows-only.
//
// References:
//   - ADR-0026 — Always Encrypted + DPAPI híbrido pra secrets locais
//   - https://learn.microsoft.com/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata
//   - https://learn.microsoft.com/windows/win32/api/dpapi/nf-dpapi-cryptunprotectdata
package dpapi

import "errors"

// ErrUnsupportedOS is returned by Protect/Unprotect on non-Windows platforms.
// The gateway boot path on Linux must not call DPAPI — it should rely on
// systemd-creds (follow-up ADR) or refuse to start.
var ErrUnsupportedOS = errors.New("DPAPI is only available on Windows")
