/**
 * Admin API client — typed fetch wrapper.
 *
 * Conventions:
 *   - All request paths are relative; the Go server hosts both the SPA and the
 *     /admin/v1 endpoints on the same origin, so no base URL is required.
 *   - The Authorization header is injected automatically from session storage.
 *   - A 401 response triggers an automatic session clear; the AuthGuard will
 *     then redirect to /ui/login on the next render cycle.
 *   - Server errors are surfaced as ApiError instances carrying the upstream
 *     status code and machine-readable error code, so handlers can branch on
 *     specific failure modes (e.g. invalid_credentials vs. internal).
 */

import { getSession, clearSession } from "./auth";

export interface ApiErrorBody {
  error: { code: string; message: string };
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

interface RequestOpts {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  /** Skip the auto Authorization header (used for the login endpoint). */
  skipAuth?: boolean;
  /** Query string parameters; undefined/null values are dropped. */
  query?: Record<string, string | number | undefined | null>;
}

async function request<T>(path: string, opts: RequestOpts = {}): Promise<T> {
  const url = new URL(path, window.location.origin);
  if (opts.query) {
    for (const [k, v] of Object.entries(opts.query)) {
      if (v !== undefined && v !== null && v !== "") {
        url.searchParams.set(k, String(v));
      }
    }
  }

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (!opts.skipAuth) {
    const s = getSession();
    if (s) headers.Authorization = `Bearer ${s.token}`;
  }

  const res = await fetch(url.toString(), {
    method: opts.method ?? "GET",
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    credentials: "same-origin",
  });

  if (res.status === 204) {
    return undefined as T;
  }

  const text = await res.text();
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = null;
    }
  }

  if (!res.ok) {
    const errBody = parsed as ApiErrorBody | null;
    const code = errBody?.error?.code ?? "unknown";
    const message =
      errBody?.error?.message ?? `request failed with status ${res.status}`;

    if (res.status === 401 && !opts.skipAuth) {
      // Token rejected — wipe local session so the guard kicks the user to login.
      clearSession();
    }

    throw new ApiError(res.status, code, message);
  }

  return parsed as T;
}

// ── Domain types ──────────────────────────────────────────────────────────────

export type Tier = "tier_1" | "tier_2" | "tier_3";
export type LBStrategy =
  | "round_robin"
  | "weighted_round_robin"
  | "random"
  | "least_connections"
  | "ip_hash";
export type AuthType = "none" | "bearer_token" | "api_key_header" | "basic_auth";

export interface Application {
  id: number;
  name: string;
  tier: Tier;
  allowed_models: string[];
  streaming_allowed: boolean;
  max_rpm: number;
  max_tpm: number;
  monthly_budget_brl: number;
  active: boolean;
  created_at: string;
  updated_at: string;
}

export interface ApplicationWithToken extends Application {
  token: string;
  key_prefix: string;
}

export interface RotateKeyResponse {
  token: string;
  key_prefix: string;
}

export interface Target {
  id: number;
  endpoint_id: number;
  url: string;
  weight: number;
  auth_type: AuthType;
  active: boolean;
  created_at: string;
}

export interface ProxyEndpoint {
  id: number;
  slug: string;
  name: string;
  lb_strategy: LBStrategy;
  max_rps: number;
  max_monthly_requests: number;
  active: boolean;
  targets: Target[];
  created_at: string;
  updated_at: string;
}

export interface AdminUser {
  id: number;
  username: string;
  role: "admin" | "operator" | "viewer";
  active: boolean;
  created_at: string;
  updated_at: string;
}

export interface UsageEvent {
  id: number;
  request_id: string;
  application_name: string;
  tier: string;
  model: string;
  provider: string;
  input_tokens: number | null;
  output_tokens: number | null;
  total_tokens: number | null;
  latency_ms: number;
  status_code: number;
  estimated_cost_brl: number | null;
  created_at: string;
}

export interface AuditEvent {
  id: number;
  request_id: string;
  application_name: string;
  event_type: string;
  severity: string;
  metadata: string | null;
  created_at: string;
}

export interface BudgetCounter {
  application_name: string;
  period: string;
  total_requests: number;
  total_tokens: number;
  estimated_cost_brl: number;
  updated_at: string;
}

export interface LoginResponse {
  token: string;
  expires_at: string;
  role: "admin" | "operator" | "viewer";
}

export interface TargetAuthInput {
  type: AuthType;
  token?: string;
  header?: string;
  value?: string;
  username?: string;
  password?: string;
}

// ── Endpoints ─────────────────────────────────────────────────────────────────

export const api = {
  // Auth
  login(username: string, password: string): Promise<LoginResponse> {
    return request("/admin/v1/auth/login", {
      method: "POST",
      body: { username, password },
      skipAuth: true,
    });
  },
  logout(): Promise<void> {
    return request("/admin/v1/auth/logout", { method: "DELETE" });
  },

  // Applications
  listApplications(): Promise<Application[]> {
    return request("/admin/v1/applications");
  },
  createApplication(
    input: Omit<Application, "id" | "active" | "created_at" | "updated_at">,
  ): Promise<ApplicationWithToken> {
    return request("/admin/v1/applications", { method: "POST", body: input });
  },
  getApplication(id: number): Promise<Application> {
    return request(`/admin/v1/applications/${id}`);
  },
  updateApplication(
    id: number,
    input: Omit<Application, "id" | "created_at" | "updated_at">,
  ): Promise<Application> {
    return request(`/admin/v1/applications/${id}`, { method: "PUT", body: input });
  },
  deleteApplication(id: number): Promise<void> {
    return request(`/admin/v1/applications/${id}`, { method: "DELETE" });
  },
  rotateKey(id: number): Promise<RotateKeyResponse> {
    return request(`/admin/v1/applications/${id}/rotate-key`, { method: "POST" });
  },
  grantAccess(appId: number, endpointId: number): Promise<void> {
    return request(`/admin/v1/applications/${appId}/grants/${endpointId}`, {
      method: "POST",
    });
  },
  revokeAccess(appId: number, endpointId: number): Promise<void> {
    return request(`/admin/v1/applications/${appId}/grants/${endpointId}`, {
      method: "DELETE",
    });
  },

  // Endpoints
  listEndpoints(): Promise<ProxyEndpoint[]> {
    return request("/admin/v1/endpoints");
  },
  createEndpoint(input: {
    slug: string;
    name: string;
    lb_strategy: LBStrategy;
    max_rps: number;
    max_monthly_requests: number;
  }): Promise<ProxyEndpoint> {
    return request("/admin/v1/endpoints", { method: "POST", body: input });
  },
  getEndpoint(id: number): Promise<ProxyEndpoint> {
    return request(`/admin/v1/endpoints/${id}`);
  },
  updateEndpoint(
    id: number,
    input: {
      slug: string;
      name: string;
      lb_strategy: LBStrategy;
      max_rps: number;
      max_monthly_requests: number;
      active: boolean;
    },
  ): Promise<ProxyEndpoint> {
    return request(`/admin/v1/endpoints/${id}`, { method: "PUT", body: input });
  },
  deleteEndpoint(id: number): Promise<void> {
    return request(`/admin/v1/endpoints/${id}`, { method: "DELETE" });
  },
  addTarget(
    endpointId: number,
    input: { url: string; weight: number; auth: TargetAuthInput },
  ): Promise<Target> {
    return request(`/admin/v1/endpoints/${endpointId}/targets`, {
      method: "POST",
      body: input,
    });
  },
  updateTarget(
    endpointId: number,
    targetId: number,
    input: {
      url: string;
      weight: number;
      auth: TargetAuthInput;
      active: boolean;
    },
  ): Promise<Target> {
    return request(`/admin/v1/endpoints/${endpointId}/targets/${targetId}`, {
      method: "PUT",
      body: input,
    });
  },
  removeTarget(endpointId: number, targetId: number): Promise<void> {
    return request(`/admin/v1/endpoints/${endpointId}/targets/${targetId}`, {
      method: "DELETE",
    });
  },

  // Users
  listUsers(): Promise<AdminUser[]> {
    return request("/admin/v1/users");
  },
  createUser(input: {
    username: string;
    password: string;
    role: "admin" | "operator" | "viewer";
  }): Promise<AdminUser> {
    return request("/admin/v1/users", { method: "POST", body: input });
  },
  deactivateUser(id: number): Promise<void> {
    return request(`/admin/v1/users/${id}`, { method: "DELETE" });
  },

  // Observability
  listUsage(params: {
    from?: string;
    to?: string;
    application?: string;
    limit?: number;
  } = {}): Promise<UsageEvent[]> {
    return request("/admin/v1/usage", { query: params });
  },
  listAudit(params: {
    from?: string;
    to?: string;
    application?: string;
    event_type?: string;
    limit?: number;
  } = {}): Promise<AuditEvent[]> {
    return request("/admin/v1/audit", { query: params });
  },
  listBudget(params: {
    period?: string;
    application?: string;
  } = {}): Promise<BudgetCounter[]> {
    return request("/admin/v1/budget", { query: params });
  },
};
