//go:build windows

package dpapi

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Protect encrypts plaintext using DPAPI with LocalMachine scope. The resulting
// ciphertext can only be decrypted on the same machine, regardless of which
// account does the Unprotect — that's what makes service accounts (gMSA) able
// to read a file encrypted at setup time by an administrator.
//
// Reasoning: CRYPTPROTECT_LOCAL_MACHINE is chosen over CurrentUser scope so
// that the operator can run the secrets CLI as Administrator while the
// gateway runs as gMSA. CurrentUser would bind the ciphertext to the operator
// account, breaking the service's ability to decrypt at boot.
//
// References:
//   - ADR-0026 §Mitigations (LocalMachine scope rationale)
//   - https://learn.microsoft.com/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata
func Protect(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("dpapi: plaintext is empty")
	}

	var in windows.DataBlob
	in.Size = uint32(len(plaintext))
	in.Data = &plaintext[0]

	var out windows.DataBlob
	err := windows.CryptProtectData(
		&in,
		nil, // szDataDescr — opcional
		nil, // optionalEntropy
		0,   // reserved
		nil, // promptStruct — nunca interativo em service context
		windows.CRYPTPROTECT_LOCAL_MACHINE|windows.CRYPTPROTECT_UI_FORBIDDEN,
		&out,
	)
	if err != nil {
		return nil, fmt.Errorf("dpapi: CryptProtectData: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))

	return cloneDataBlob(&out), nil
}

// Unprotect reverses Protect. The ciphertext MUST have been produced on the
// same machine (LocalMachine scope); cross-machine recovery requires the PFX
// backup of the DPAPI master key, which is not in scope here.
//
// References:
//   - ADR-0026
//   - https://learn.microsoft.com/windows/win32/api/dpapi/nf-dpapi-cryptunprotectdata
func Unprotect(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("dpapi: ciphertext is empty")
	}

	var in windows.DataBlob
	in.Size = uint32(len(ciphertext))
	in.Data = &ciphertext[0]

	var out windows.DataBlob
	err := windows.CryptUnprotectData(
		&in,
		nil, // name (output pointer to szDataDescr) — descartamos
		nil, // optionalEntropy
		0,   // reserved
		nil, // promptStruct
		windows.CRYPTPROTECT_LOCAL_MACHINE|windows.CRYPTPROTECT_UI_FORBIDDEN,
		&out,
	)
	if err != nil {
		return nil, fmt.Errorf("dpapi: CryptUnprotectData: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))

	return cloneDataBlob(&out), nil
}

// cloneDataBlob copies the Windows-allocated buffer into a Go-owned slice so
// the caller can return safely after LocalFree fires.
func cloneDataBlob(b *windows.DataBlob) []byte {
	if b.Size == 0 || b.Data == nil {
		return nil
	}
	src := unsafe.Slice(b.Data, int(b.Size))
	dst := make([]byte, b.Size)
	copy(dst, src)
	return dst
}
