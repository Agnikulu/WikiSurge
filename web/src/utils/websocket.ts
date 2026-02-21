function getWsBaseUrl(): string {
  if (import.meta.env.VITE_WS_URL) return import.meta.env.VITE_WS_URL;
  if (typeof window !== 'undefined' && window.location.hostname !== 'localhost') {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${proto}//${window.location.host}`;
  }
  return 'ws://localhost:8081';
}

const WS_BASE_URL = getWsBaseUrl();

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
  feed: '/ws/feed',
  edits: '/ws/feed',
  alerts: '/ws/alerts',
} as const;
