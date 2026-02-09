import { formatDistanceToNowStrict, parseISO } from 'date-fns';
import type { Edit } from '../types';

/**
 * Format an ISO timestamp to a relative time string (e.g., "2s ago").
 */
export function formatTimestamp(timestamp: string | undefined | null): string {
  if (!timestamp) return 'unknown';
  try {
    const date = parseISO(timestamp);
    if (isNaN(date.getTime())) return timestamp;
    return formatDistanceToNowStrict(date, { addSuffix: true });
  } catch {
    return timestamp;
  }
}

/**
 * Format a timestamp to a compact relative string (e.g., "2s ago").
 * Accepts either a Unix timestamp (number of seconds) or an ISO date string.
 */
export function formatRelativeTime(timestamp: number | string): string {
  try {
    let epochMs: number;
    if (typeof timestamp === 'number') {
      epochMs = timestamp * 1000;
    } else {
      epochMs = new Date(timestamp).getTime();
    }

    const now = Date.now();
    const diffMs = now - epochMs;
    if (diffMs < 0) return 'just now';

    const seconds = Math.floor(diffMs / 1000);
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
  } catch {
    return String(timestamp);
  }
}

/** @deprecated Use formatRelativeTime instead */
export const formatUnixTimestamp = formatRelativeTime;

/**
 * Compute byte change from an Edit's length field.
 */
export function getByteChange(edit: Edit): number {
  if (edit.byte_change !== undefined) return edit.byte_change;
  return (edit.length?.new ?? 0) - (edit.length?.old ?? 0);
}

/**
 * Extract language code from wiki field (e.g., "enwiki" → "en").
 */
export function extractLanguage(wiki: string): string {
  if (!wiki || wiki.length < 2) return wiki;
  return wiki.replace(/wiki$/, '');
}

/**
 * Check if an edit created a new page (old length is 0).
 */
export function isNewPage(edit: Edit): boolean {
  return edit.length?.old === 0 && edit.length?.new > 0;
}

/**
 * Build a Wikipedia URL for a page title on a given wiki.
 */
export function buildWikiUrl(title: string, serverUrl?: string): string {
  if (!title) return '#';
  const base = serverUrl || 'https://en.wikipedia.org';
  return `${base}/wiki/${encodeURIComponent(title.replace(/ /g, '_'))}`;
}

/**
 * Build a Wikipedia diff URL for given revision IDs.
 */
export function buildDiffUrl(revOld: number, revNew: number, serverUrl?: string): string {
  const base = serverUrl || 'https://en.wikipedia.org';
  return `${base}/w/index.php?diff=${revNew}&oldid=${revOld}`;
}

/**
 * Build a Wikipedia user page URL.
 */
export function buildUserUrl(user: string, serverUrl?: string): string {
  const base = serverUrl || 'https://en.wikipedia.org';
  return `${base}/wiki/User:${encodeURIComponent(user.replace(/ /g, '_'))}`;
}

/**
 * Format large numbers with commas (e.g., 123456 -> "123,456").
 */
export function formatNumber(num: number): string {
  if (num == null || isNaN(num)) return '0';
  return num.toLocaleString('en-US');
}

/**
 * Format byte changes with +/- and human-readable units (e.g., +1.2K, -500).
 */
export function formatByteChange(bytes: number): string {
  const sign = bytes >= 0 ? '+' : '';
  const abs = Math.abs(bytes);

  if (abs >= 1_000_000) {
    return `${sign}${(bytes / 1_000_000).toFixed(1)}M`;
  }
  if (abs >= 1_000) {
    return `${sign}${(bytes / 1_000).toFixed(1)}K`;
  }
  return `${sign}${bytes}`;
}

/**
 * Map severity levels to Tailwind CSS color classes.
 */
export function getSeverityColor(severity: string): {
  bg: string;
  text: string;
  border: string;
  dot: string;
} {
  switch (severity.toLowerCase()) {
    case 'critical':
      return {
        bg: 'bg-red-900/20',
        text: 'text-red-400',
        border: 'border-red-800/40',
        dot: 'bg-red-500',
      };
    case 'high':
      return {
        bg: 'bg-amber-900/20',
        text: 'text-amber-400',
        border: 'border-amber-800/40',
        dot: 'bg-amber-500',
      };
    case 'medium':
      return {
        bg: 'bg-emerald-900/20',
        text: 'text-emerald-400',
        border: 'border-emerald-800/40',
        dot: 'bg-emerald-500',
      };
    case 'low':
    default:
      return {
        bg: 'bg-cyan-900/20',
        text: 'text-cyan-400',
        border: 'border-cyan-800/40',
        dot: 'bg-cyan-500',
      };
  }
}

/**
 * Truncate long page titles with ellipsis.
 */
export function truncateTitle(title: string, maxLength = 60): string {
  if (title.length <= maxLength) return title;
  return `${title.slice(0, maxLength)}…`;
}

/**
 * Get a CSS class for byte change coloring (green for additions, red for deletions).
 */
export function getByteChangeColor(bytes: number): string {
  if (bytes > 0) return 'text-emerald-400';
  if (bytes < 0) return 'text-red-400';
  return 'text-gray-500';
}
