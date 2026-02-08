const WS_BASE_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:8080';

export function buildWebSocketUrl(path: string, params?: Record<string, string>): string {
  const url = new URL(path, WS_BASE_URL);
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value) url.searchParams.set(key, value);
    });
  }
  return url.toString();
}

export const WS_ENDPOINTS = {
  edits: '/ws/edits',
  alerts: '/ws/alerts',
} as const;
