package handlers

import (
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/infra/mssql"
)

// NOTE: this file's exported name (translatePgError) was kept from the PG era
// to minimize blast radius for callers, but the underlying inspection now
// works on mssql.Error (ADR-0022).
//
// translatePgError mapeia erros conhecidos do SQL Server em (statusCode, code, message)
// amigáveis ao operador. Quando o erro NÃO é do driver mssql ou o Number não está
// catalogado, retorna ok=false e o caller deve usar o caminho genérico (500 +
// details com err.Error()).
//
// SQL Server error numbers relevantes:
//   - 2627 PRIMARY KEY / UNIQUE constraint violation → 409 (análogo a PG 23505)
//   - 2601 unique index violation (filtered)          → 409
//   - 547  CHECK / FOREIGN KEY violation              → 400
//
// Reasoning: o nome do constraint vem no Message do mssql.Error (e.g.
// "Violation of UNIQUE KEY constraint 'uq_applications_name'. Cannot insert
// duplicate key in object 'gogateway.applications'."). Inspecionamos a Message
// para devolver mensagens em português específicas — mesma estratégia que
// usávamos com pgErr.ConstraintName.
//
// References:
//   - ADR-0022 — substituição PG → SQL Server
//   - CLAUDE.md §5.4 — erros sempre wrapped + mensagens claras
//   - https://learn.microsoft.com/en-us/sql/relational-databases/errors-events/database-engine-events-and-errors
func translatePgError(err error) (status int, code, message, details string, ok bool) {
	num := mssql.MSSQLErrorNumber(err)
	if num == 0 {
		return 0, "", "", "", false
	}

	msg := mssql.MSSQLErrorMessage(err)

	switch num {
	case mssql.ErrNumberDuplicateKey, mssql.ErrNumberUniqueIndexViolation:
		return 409, "duplicate", humanUniqueViolation(msg), msg, true
	case mssql.ErrNumberConstraintViolation:
		// 547 cobre tanto CHECK quanto FOREIGN KEY — a mensagem distingue.
		if strings.Contains(strings.ToUpper(msg), "FOREIGN KEY") {
			return 400, "invalid_reference", "referência a registro inexistente", msg, true
		}
		return 400, "invalid_value", humanCheckViolation(msg), msg, true
	default:
		return 0, "", "", "", false
	}
}

// humanUniqueViolation deduz qual constraint violou a partir da mensagem do
// SQL Server (que cita o nome do constraint entre aspas simples).
// Cobre as constraints relevantes do admin plane.
func humanUniqueViolation(msg string) string {
	upper := strings.ToUpper(msg)
	switch {
	case strings.Contains(upper, "UQ_PROXY_ENDPOINTS_SLUG"):
		return "já existe um endpoint com esse slug — escolha outro identificador"
	case strings.Contains(upper, "UQ_APPLICATIONS_NAME"):
		return "já existe uma aplicação com esse nome"
	case strings.Contains(upper, "UQ_ADMIN_USERS_USERNAME"):
		return "já existe um usuário com esse username"
	case strings.Contains(upper, "UQ_API_KEYS_APPLICATION"):
		return "essa aplicação já possui uma chave ativa"
	case strings.Contains(upper, "IDX_API_KEYS_ACTIVE_PER_APP"):
		return "essa aplicação já possui uma chave ativa"
	case strings.Contains(upper, "IDX_API_KEYS_ACTIVE_PREFIX"):
		return "o nome da aplicação produz um prefixo de chave (gwk_…) que colide " +
			"com outra aplicação ativa — escolha um nome mais distinto nos primeiros caracteres"
	case strings.Contains(upper, "UQ_ADMIN_SESSIONS_TOKEN"):
		return "colisão de token de sessão — tente novamente"
	case strings.Contains(upper, "UQ_BUDGET_COUNTERS_APP_PERIOD"):
		return "já existe contador de budget para essa aplicação no período"
	default:
		return "valor duplicado em campo único"
	}
}

// humanCheckViolation mapeia constraints CHECK conhecidas em mensagens claras.
func humanCheckViolation(msg string) string {
	upper := strings.ToUpper(msg)
	switch {
	case strings.Contains(upper, "CK_PROXY_ENDPOINTS_PROVIDER_KIND"):
		return "provider_kind inválido — use um dos valores conhecidos ou 'custom'"
	case strings.Contains(upper, "CK_PROXY_ENDPOINTS_LB_STRATEGY"):
		return "estratégia de balanceamento inválida"
	case strings.Contains(upper, "CK_APPLICATIONS_TIER"):
		return "tier inválido — use tier_1, tier_2 ou tier_3"
	case strings.Contains(upper, "CK_ADMIN_USERS_ROLE"):
		return "role inválido — use admin, operator ou viewer"
	case strings.Contains(upper, "CK_PROXY_TARGETS_WEIGHT"):
		return "peso do target precisa ser maior que zero"
	case strings.Contains(upper, "CK_PROXY_TARGETS_AUTH_TYPE"):
		return "tipo de autenticação inválido"
	case strings.Contains(upper, "CK_APPLICATIONS_ALLOWED_MODELS"),
		strings.Contains(upper, "CK_PROXY_ENDPOINTS_PROVIDER_CONFIG"):
		return "campo JSON inválido"
	default:
		return "valor inválido para o campo (violou check constraint)"
	}
}
