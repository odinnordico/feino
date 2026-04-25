import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** Merge Tailwind classes safely. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** Format a millisecond duration for display. */
export function formatMs(ms: number): string {
  if (ms < 1000) {return `${Math.round(ms)}ms`;}
  return `${(ms / 1000).toFixed(1)}s`;
}

/** Truncate a string to maxLen with an ellipsis. */
export function truncate(text: string, maxLen: number): string {
  if (text.length <= maxLen) {return text;}
  return `${text.slice(0, maxLen)  }…`;
}

/** Format a token count, e.g. 1234 → "1.2k". */
export function formatTokens(n: number): string {
  if (n < 1000) {return String(n);}
  return `${(n / 1000).toFixed(1)}k`;
}

/** Return true if a string is non-empty after trimming. */
export function notEmpty(s: string | undefined | null): s is string {
  return typeof s === "string" && s.trim().length > 0;
}

/** Format a byte count for display, e.g. 1024 → "1.0K". */
export function formatBytes(n: number): string {
  if (n < 1024) {return `${n}B`;}
  if (n < 1024 * 1024) {return `${(n / 1024).toFixed(1)}K`;}
  return `${(n / 1024 / 1024).toFixed(1)}M`;
}
