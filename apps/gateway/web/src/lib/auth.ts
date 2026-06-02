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

// Snapshot cache — useSyncExternalStore compares snapshots with Object.is, so
// we MUST return the same reference until something actually changes. Building
// a fresh { token, expiresAt, role } on every read would trigger an infinite
// render loop (React error #185).
let cachedSnapshot: Session | null = null;
let cachedKey = "";

function buildSnapshot(): { session: Session | null; key: string } {
  let token: string | null = null;
  let expiresAt: string | null = null;
  let role: Role | null = null;
  try {
    token = sessionStorage.getItem(TOKEN_KEY);
    expiresAt = sessionStorage.getItem(EXPIRES_KEY);
    role = sessionStorage.getItem(ROLE_KEY) as Role | null;
  } catch {
    // sessionStorage pode ser bloqueado (private mode, quota cheia, etc.).
    return { session: null, key: "no-storage" };
  }
  const key = `${token ?? ""}|${expiresAt ?? ""}|${role ?? ""}`;

  if (!token || !expiresAt || !role) {
    return { session: null, key };
  }

  // role precisa ser um dos valores conhecidos; valor inesperado = storage corrompido.
  if (role !== "admin" && role !== "operator" && role !== "viewer") {
    return { session: null, key: "invalid-role" };
  }

  const expiry = Date.parse(expiresAt);
  if (Number.isNaN(expiry)) {
    return { session: null, key: "invalid-expiry" };
  }
  if (expiry < Date.now()) {
    return { session: null, key: "expired" };
  }
  return { session: { token, expiresAt, role }, key };
}

function refreshCache(): void {
  const next = buildSnapshot();
  if (next.key !== cachedKey) {
    cachedSnapshot = next.session;
    cachedKey = next.key;
  }
}

function emit(): void {
  refreshCache();
  listeners.forEach((fn) => {
    try {
      fn();
    } catch {
      /* listeners must not break notify */
    }
  });
}

// Initial population so the first getSession() call returns the right snapshot.
refreshCache();

// React to setSession/clearSession from other tabs of the same browser.
// Without this, two tabs of the console could disagree on auth state.
if (typeof window !== "undefined") {
  window.addEventListener("storage", () => emit());
}

/** subscribe to session changes; returns an unsubscribe function. */
export function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/**
 * Returns the current session or null if not logged in / expired.
 * Stable reference across renders — useSyncExternalStore depends on this.
 *
 * The cache is invalidated by setSession/clearSession (via emit) and by the
 * "storage" window event (cross-tab updates). Stale expiry is also detected
 * here because Date.parse(expiresAt) is part of the cache key.
 */
export function getSession(): Session | null {
  // Cheap path: re-check expiry against the wall clock without rebuilding.
  if (cachedSnapshot && Date.parse(cachedSnapshot.expiresAt) < Date.now()) {
    clearSession();
  }
  return cachedSnapshot;
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
