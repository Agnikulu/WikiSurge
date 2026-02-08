import { formatDistanceToNow, parseISO } from 'date-fns';

/**
 * Format an ISO timestamp to a relative time string (e.g., "2 minutes ago").
 */
export function formatTimestamp(timestamp: string): string {
  try {
    const date = parseISO(timestamp);
    return formatDistanceToNow(date, { addSuffix: true });
  } catch {
    return timestamp;
  }
}

/**
 * Format large numbers with commas (e.g., 123456 -> "123,456").
 */
export function formatNumber(num: number): string {
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
        bg: 'bg-red-50',
        text: 'text-red-800',
        border: 'border-red-200',
        dot: 'bg-red-500',
      };
    case 'high':
      return {
        bg: 'bg-orange-50',
        text: 'text-orange-800',
        border: 'border-orange-200',
        dot: 'bg-orange-500',
      };
    case 'medium':
      return {
        bg: 'bg-yellow-50',
        text: 'text-yellow-800',
        border: 'border-yellow-200',
        dot: 'bg-yellow-500',
      };
    case 'low':
    default:
      return {
        bg: 'bg-blue-50',
        text: 'text-blue-800',
        border: 'border-blue-200',
        dot: 'bg-blue-500',
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
  if (bytes > 0) return 'text-green-600';
  if (bytes < 0) return 'text-red-600';
  return 'text-gray-500';
}
