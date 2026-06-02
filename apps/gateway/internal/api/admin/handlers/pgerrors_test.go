package handlers

import (
	"errors"
	"fmt"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"
)

// Test names preserve the historical "PgError" prefix even after the migration
// to SQL Server (ADR-0022) because translatePgError continues to be the public
// helper name in this package — renaming it would force a larger blast radius
// at every handler. Internally the helper now inspects mssql.Error numbers.

// TestTranslatePgError covers the main error-translation matrix for SQL Server
// driver errors (microsoft/go-mssqldb).
func TestTranslatePgError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantOK        bool
		wantStatus    int
		wantCode      string
		wantMsgPrefix string
	}{
		{
			name:   "non mssql error",
			err:    fmt.Errorf("random error"),
			wantOK: false,
		},
		{
			name: "unique violation on endpoint slug (2627)",
			err: mssql.Error{
				Number:  2627,
				Message: "Violation of UNIQUE KEY constraint 'uq_proxy_endpoints_slug'. Cannot insert duplicate key in object 'gogateway.proxy_endpoints'.",
			},
			wantOK:        true,
			wantStatus:    409,
			wantCode:      "duplicate",
			wantMsgPrefix: "já existe um endpoint",
		},
		{
			name: "unique violation on user username (2627)",
			err: mssql.Error{
				Number:  2627,
				Message: "Violation of UNIQUE KEY constraint 'uq_admin_users_username'.",
			},
			wantOK:        true,
			wantStatus:    409,
			wantCode:      "duplicate",
			wantMsgPrefix: "já existe um usuário",
		},
		{
			name: "filtered unique index violation on key_prefix (2601)",
			err: mssql.Error{
				Number:  2601,
				Message: "Cannot insert duplicate key row in object 'gogateway.api_keys' with unique index 'idx_api_keys_active_prefix'.",
			},
			wantOK:        true,
			wantStatus:    409,
			wantCode:      "duplicate",
			wantMsgPrefix: "prefixo de chave",
		},
		{
			name: "check violation on provider_kind (547)",
			err: mssql.Error{
				Number:  547,
				Message: "The INSERT statement conflicted with the CHECK constraint 'ck_proxy_endpoints_provider_kind'.",
			},
			wantOK:        true,
			wantStatus:    400,
			wantCode:      "invalid_value",
			wantMsgPrefix: "provider_kind inválido",
		},
		{
			name: "foreign key violation (547)",
			err: mssql.Error{
				Number:  547,
				Message: "The INSERT statement conflicted with the FOREIGN KEY constraint 'fk_grants_application'.",
			},
			wantOK:        true,
			wantStatus:    400,
			wantCode:      "invalid_reference",
			wantMsgPrefix: "referência",
		},
		{
			name: "unknown mssql error number",
			err: mssql.Error{
				Number:  99999,
				Message: "some unmapped server-side error",
			},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, code, msg, _, ok := translatePgError(tc.err)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Errorf("code = %q, want %q", code, tc.wantCode)
			}
			if msg == "" {
				t.Error("message is empty")
			}
			if tc.wantMsgPrefix != "" && !contains(msg, tc.wantMsgPrefix) {
				t.Errorf("msg = %q, want to contain %q", msg, tc.wantMsgPrefix)
			}
		})
	}
}

// TestTranslatePgError_WrappedError confirms that fmt.Errorf("...: %w", err)
// wrappers are unwrapped correctly via errors.As inside translatePgError.
func TestTranslatePgError_WrappedError(t *testing.T) {
	inner := mssql.Error{
		Number:  2627,
		Message: "Violation of UNIQUE KEY constraint 'uq_proxy_endpoints_slug'.",
	}
	wrapped := fmt.Errorf("inserting endpoint: %w", inner)

	_, code, _, _, ok := translatePgError(wrapped)
	if !ok {
		t.Fatal("expected wrapped mssql error to be recognized")
	}
	if code != "duplicate" {
		t.Errorf("code = %q, want duplicate", code)
	}

	// errors.As-compatibility check.
	var mssqlErr mssql.Error
	if !errors.As(wrapped, &mssqlErr) {
		t.Error("errors.As broken — mssql.Error not extractable from wrapped error")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
