package handlers

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// translatePgError mapeia erros conhecidos do Postgres em (statusCode, code, message)
// amigáveis ao operador. Quando o erro NÃO é de Postgres ou o SQLSTATE não está
// catalogado, retorna ok=false e o caller deve usar o caminho genérico (500 +
// details com err.Error()).
//
// Códigos SQLSTATE relevantes:
//   - 23505 unique_violation        → 409 conflict, "slug/nome já existe"
//   - 23514 check_violation         → 400 bad_request, "valor inválido para campo X"
//   - 23502 not_null_violation      → 400 bad_request, "campo obrigatório ausente"
//   - 23503 foreign_key_violation   → 400 bad_request, "referência inválida"
//
// Reasoning: surfar SQLSTATE direto ao usuário expõe internals e gera mensagens
// confusas ("duplicate key value violates unique constraint
// proxy_endpoints_slug_key" não diz muito pra um operador). O mapeamento aqui
// faz o trabalho de tradução em um único lugar.
//
// References:
//   - https://www.postgresql.org/docs/17/errcodes-appendix.html
//   - CLAUDE.md §5.4 — erros sempre wrapped + mensagens claras
func translatePgError(err error) (status int, code, message, details string, ok bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return 0, "", "", "", false
	}

	switch pgErr.Code {
	case "23505": // unique_violation
		return 409, "duplicate", humanUniqueViolation(pgErr), pgErr.Message, true
	case "23514": // check_violation
		return 400, "invalid_value", humanCheckViolation(pgErr), pgErr.Message, true
	case "23502": // not_null_violation
		return 400, "missing_field", "o campo " + pgErr.ColumnName + " é obrigatório", pgErr.Message, true
	case "23503": // foreign_key_violation
		return 400, "invalid_reference", "referência a registro inexistente", pgErr.Message, true
	default:
		return 0, "", "", "", false
	}
}

// humanUniqueViolation deduz qual constraint violou a partir do nome do índice
// (Postgres expõe via ConstraintName / Message). Cobre as constraints relevantes
// do admin plane.
func humanUniqueViolation(pg *pgconn.PgError) string {
	name := pg.ConstraintName
	switch {
	case strings.Contains(name, "proxy_endpoints_slug"):
		return "já existe um endpoint com esse slug — escolha outro identificador"
	case strings.Contains(name, "applications_name"):
		return "já existe uma aplicação com esse nome"
	case strings.Contains(name, "admin_users_username"):
		return "já existe um usuário com esse username"
	case strings.Contains(name, "api_keys_application_id"):
		return "essa aplicação já possui uma chave ativa"
	case strings.Contains(name, "api_keys_active_prefix"):
		return "o nome da aplicação produz um prefixo de chave (gwk_…) que colide " +
			"com outra aplicação ativa — escolha um nome mais distinto nos primeiros caracteres"
	case strings.Contains(name, "admin_sessions_token_hash"):
		return "colisão de token de sessão — tente novamente"
	default:
		return "valor duplicado em campo único"
	}
}

// humanCheckViolation mapeia constraints CHECK conhecidas em mensagens claras.
func humanCheckViolation(pg *pgconn.PgError) string {
	name := pg.ConstraintName
	switch {
	case strings.Contains(name, "provider_kind"):
		return "provider_kind inválido — use um dos valores conhecidos ou 'custom'"
	case strings.Contains(name, "lb_strategy"):
		return "estratégia de balanceamento inválida"
	case strings.Contains(name, "tier"):
		return "tier inválido — use tier_1, tier_2 ou tier_3"
	case strings.Contains(name, "role"):
		return "role inválido — use admin, operator ou viewer"
	case strings.Contains(name, "weight"):
		return "peso do target precisa ser maior que zero"
	case strings.Contains(name, "auth_type"):
		return "tipo de autenticação inválido"
	default:
		return "valor inválido para o campo (violou check constraint)"
	}
}

