package handlers

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestTranslatePgError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantOK         bool
		wantStatus     int
		wantCode       string
		wantMsgPrefix  string // checked via strings.Contains for flexibility
	}{
		{
			name:   "non pg error",
			err:    fmt.Errorf("random error"),
			wantOK: false,
		},
		{
			name: "unique violation on endpoint slug",
			err: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "proxy_endpoints_slug_key",
				Message:        "duplicate key value violates unique constraint",
			},
			wantOK:        true,
			wantStatus:    409,
			wantCode:      "duplicate",
			wantMsgPrefix: "já existe um endpoint",
		},
		{
			name: "unique violation on user username",
			err: &pgconn.PgError{
				Code:           "23505",
				ConstraintName: "admin_users_username_key",
				Message:        "duplicate key value violates unique constraint",
			},
			wantOK:        true,
			wantStatus:    409,
			wantCode:      "duplicate",
			wantMsgPrefix: "já existe um usuário",
		},
		{
			name: "check violation on provider_kind",
			err: &pgconn.PgError{
				Code:           "23514",
				ConstraintName: "proxy_endpoints_provider_kind_check",
				Message:        "violates check constraint",
			},
			wantOK:        true,
			wantStatus:    400,
			wantCode:      "invalid_value",
			wantMsgPrefix: "provider_kind inválido",
		},
		{
			name: "not null violation",
			err: &pgconn.PgError{
				Code:       "23502",
				ColumnName: "slug",
				Message:    "null value in column",
			},
			wantOK:        true,
			wantStatus:    400,
			wantCode:      "missing_field",
			wantMsgPrefix: "campo slug",
		},
		{
			name: "foreign key violation",
			err: &pgconn.PgError{
				Code:    "23503",
				Message: "foreign key violation",
			},
			wantOK:        true,
			wantStatus:    400,
			wantCode:      "invalid_reference",
			wantMsgPrefix: "referência",
		},
		{
			name: "unknown pg error code",
			err: &pgconn.PgError{
				Code:    "42P01",
				Message: "relation does not exist",
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

func TestTranslatePgError_WrappedError(t *testing.T) {
	inner := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "proxy_endpoints_slug_key",
		Message:        "dup",
	}
	wrapped := fmt.Errorf("inserting endpoint: %w", inner)

	_, code, _, _, ok := translatePgError(wrapped)
	if !ok {
		t.Fatal("expected wrapped pg error to be recognized")
	}
	if code != "duplicate" {
		t.Errorf("code = %q, want duplicate", code)
	}

	// And errors.As-compatibility check:
	var pg *pgconn.PgError
	if !errors.As(wrapped, &pg) {
		t.Error("errors.As broken — pg error not extractable")
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
