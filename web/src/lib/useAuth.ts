import { useSyncExternalStore } from "react";
import { getSession, subscribe, type Session } from "./auth";

/**
 * useSession — React hook that subscribes to the in-memory session state.
 * Re-renders automatically when the session changes (login, logout, expiry).
 */
export function useSession(): Session | null {
  return useSyncExternalStore(subscribe, getSession, () => null);
}
