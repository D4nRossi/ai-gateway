-- 004_proxy_endpoints.up.sql (T-SQL)
--
-- Portado de migrations/postgres-legacy/004_proxy_endpoints.up.sql (ADR-0022).
-- Tabelas para o motor genérico de proxy HTTP: proxy_endpoints, proxy_targets
-- e application_endpoint_grants (ADR-0010, ADR-0013).
--
-- Nota sobre BYTEA → VARBINARY(MAX): auth_config_enc guarda o ciphertext
-- AES-256-GCM (12-byte nonce || GCM ciphertext) das credenciais do target
-- (ADR-0012). NULL quando auth_type='none'.
--
-- Nota sobre múltiplos CASCADE em application_endpoint_grants: SQL Server
-- aceita múltiplas FK ON DELETE CASCADE chegando à mesma tabela contanto
-- que NÃO haja ciclo lógico. Os dois caminhos aqui (applications → grants
-- e proxy_endpoints → grants) não convergem por nenhum outro caminho, então
-- a configuração é segura.

IF OBJECT_ID('gogateway.proxy_endpoints', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.proxy_endpoints (
        id                    BIGINT          IDENTITY(1,1) PRIMARY KEY,
        -- Identificador URL-safe usado em /v1/proxy/{slug}
        slug                  NVARCHAR(64)    NOT NULL,
        name                  NVARCHAR(128)   NOT NULL,
        lb_strategy           NVARCHAR(30)    NOT NULL DEFAULT 'round_robin'
                                CONSTRAINT ck_proxy_endpoints_lb_strategy CHECK (lb_strategy IN (
                                    'round_robin', 'weighted_round_robin',
                                    'random', 'least_connections', 'ip_hash'
                                )),
        -- 0 = ilimitado
        max_rps               INT             NOT NULL DEFAULT 0,
        max_monthly_requests  BIGINT          NOT NULL DEFAULT 0,
        active                BIT             NOT NULL DEFAULT 1,
        created_at            DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at            DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT uq_proxy_endpoints_slug UNIQUE (slug)
    );

    CREATE INDEX idx_proxy_endpoints_slug
        ON gogateway.proxy_endpoints(slug)
        WHERE active = 1;
END;

IF OBJECT_ID('gogateway.proxy_targets', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.proxy_targets (
        id               BIGINT          IDENTITY(1,1) PRIMARY KEY,
        endpoint_id      BIGINT          NOT NULL,
        url              NVARCHAR(MAX)   NOT NULL,
        weight           INT             NOT NULL DEFAULT 1
                             CONSTRAINT ck_proxy_targets_weight CHECK (weight > 0),
        auth_type        NVARCHAR(20)    NOT NULL DEFAULT 'none'
                             CONSTRAINT ck_proxy_targets_auth_type CHECK (auth_type IN ('none', 'bearer_token', 'api_key_header', 'basic_auth')),
        -- AES-256-GCM ciphertext (12-byte nonce || GCM ciphertext); NULL quando auth_type='none'.
        auth_config_enc  VARBINARY(MAX)  NULL,
        active           BIT             NOT NULL DEFAULT 1,
        created_at       DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_proxy_targets_endpoint
            FOREIGN KEY (endpoint_id) REFERENCES gogateway.proxy_endpoints(id) ON DELETE CASCADE
    );

    CREATE INDEX idx_proxy_targets_endpoint
        ON gogateway.proxy_targets(endpoint_id)
        WHERE active = 1;
END;

IF OBJECT_ID('gogateway.application_endpoint_grants', 'U') IS NULL
BEGIN
    CREATE TABLE gogateway.application_endpoint_grants (
        application_id  BIGINT          NOT NULL,
        endpoint_id     BIGINT          NOT NULL,
        created_at      DATETIMEOFFSET  NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT pk_application_endpoint_grants PRIMARY KEY (application_id, endpoint_id),
        CONSTRAINT fk_grants_application FOREIGN KEY (application_id) REFERENCES gogateway.applications(id) ON DELETE CASCADE,
        CONSTRAINT fk_grants_endpoint    FOREIGN KEY (endpoint_id)    REFERENCES gogateway.proxy_endpoints(id) ON DELETE CASCADE
    );

    CREATE INDEX idx_grants_endpoint
        ON gogateway.application_endpoint_grants(endpoint_id);
END;
