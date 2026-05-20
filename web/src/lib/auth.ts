/**
 * Auth state lives in sessionStorage (cleared when the tab closes) — not
 * localStorage. This caps token exposure to the active browsing session and
 * matches docs/v2-alignment.md.
 *
 * The module is intentionally framework-agnostic so any component can read
 * the current state; React components subscribe via a useSyncExternalStore
 * adapter (see useAuth.ts).
 */

const TOKEN_KEY = "ai_gateway_token";
const EXPIRES_KEY = "ai_gateway_expires";
const ROLE_KEY = "ai_gateway_role";

export type Role = "admin" | "operator" | "viewer";

export interface Session {
  token: string;
  expiresAt: string;
  role: Role;
}

const listeners = new Set<() => void>();

function emit(): void {
  listeners.forEach((fn) => {
    try {
      fn();
    } catch {
      /* listeners must not break notify */
    }
  });
}

/** subscribe to session changes; returns an unsubscribe function. */
export function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Returns the current session or null if not logged in / expired. */
export function getSession(): Session | null {
  const token = sessionStorage.getItem(TOKEN_KEY);
  const expiresAt = sessionStorage.getItem(EXPIRES_KEY);
  const role = sessionStorage.getItem(ROLE_KEY) as Role | null;
  if (!token || !expiresAt || !role) return null;
  // Defensive: if the stored expiry is in the past, treat as logged out.
  if (Date.parse(expiresAt) < Date.now()) {
    clearSession();
    return null;
  }
  return { token, expiresAt, role };
}

export function setSession(s: Session): void {
  sessionStorage.setItem(TOKEN_KEY, s.token);
  sessionStorage.setItem(EXPIRES_KEY, s.expiresAt);
  sessionStorage.setItem(ROLE_KEY, s.role);
  emit();
}

export function clearSession(): void {
  sessionStorage.removeItem(TOKEN_KEY);
  sessionStorage.removeItem(EXPIRES_KEY);
  sessionStorage.removeItem(ROLE_KEY);
  emit();
}

/** Role hierarchy mirroring the backend RequireRole middleware. */
const RANK: Record<Role, number> = { viewer: 0, operator: 1, admin: 2 };

export function hasRole(required: Role): boolean {
  const s = getSession();
  if (!s) return false;
  return RANK[s.role] >= RANK[required];
}
