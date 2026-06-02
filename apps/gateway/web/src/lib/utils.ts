import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * cn — concatenates tailwind class names and resolves conflicts (last wins).
 * Standard helper used by every shadcn-style component.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

/** Format an RFC3339 timestamp as the locale's short date+time string. */
export function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString("pt-BR", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

/** Format a number as BRL currency (R$ 1.234,56). */
export function formatBRL(value: number): string {
  return new Intl.NumberFormat("pt-BR", {
    style: "currency",
    currency: "BRL",
    minimumFractionDigits: 2,
  }).format(value);
}

/** Format an integer with thousand separators. */
export function formatNumber(value: number): string {
  return new Intl.NumberFormat("pt-BR").format(value);
}
