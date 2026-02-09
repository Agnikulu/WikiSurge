import axios from 'axios';
import type { TrendingPage, Edit, Alert, Stats, EditWar, SearchResult, SearchParams } from '../types';

const API_BASE_URL = import.meta.env.VITE_API_URL || '';

const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-cache',
  },
});

// Add request/response interceptors for error handling
api.interceptors.response.use(
  (response) => response,
  (error) => {
    console.error('API Error:', error);
    return Promise.reject(error);
  }
);

// API methods

export const getTrending = async (limit = 20, language?: string): Promise<TrendingPage[]> => {
  const params: Record<string, unknown> = { limit };
  if (language) params.language = language;
  const response = await api.get('/api/trending', { params });
  return response.data;
};

export const searchEdits = async (
  query: string,
  limit = 50,
  params?: Partial<SearchParams>
): Promise<SearchResult> => {
  const searchParams: Record<string, unknown> = { q: query, limit };
  if (params?.offset) searchParams.offset = params.offset;
  if (params?.sort) searchParams.sort = params.sort;
  if (params?.from) searchParams.from = params.from;
  if (params?.to) searchParams.to = params.to;
  if (params?.language) searchParams.language = params.language;
  if (params?.wiki) searchParams.wiki = params.wiki;
  if (params?.user) searchParams.user = params.user;
  if (params?.exclude_bots) searchParams.exclude_bots = params.exclude_bots;
  if (params?.min_bytes !== undefined) searchParams.min_bytes = params.min_bytes;
  if (params?.max_bytes !== undefined) searchParams.max_bytes = params.max_bytes;
  const response = await api.get('/api/search', { params: searchParams });
  // Backend returns 'hits' but frontend expects 'edits'
  const data = response.data;
  return {
    edits: data.hits || [],
    total: data.total || 0,
  };
};

export const getAlerts = async (limit = 20, since?: string): Promise<Alert[]> => {
  const params: Record<string, unknown> = { limit };
  if (since) params.since = since;
  const response = await api.get('/api/alerts', { params });
  // The backend wraps alerts in {alerts:[], total, pagination}.
  const raw = response.data;
  if (Array.isArray(raw)) return raw;
  if (raw && Array.isArray(raw.alerts)) return raw.alerts;
  return [];
};

export const getStats = async (): Promise<Stats> => {
  const response = await api.get('/api/stats');
  return response.data;
};

export const getEditWars = async (active = true): Promise<EditWar[]> => {
  const response = await api.get('/api/edit-wars', {
    params: { active },
  });
  return response.data;
};

export const getRecentEdits = async (limit = 50): Promise<Edit[]> => {
  const response = await api.get('/api/edits/recent', {
    params: { limit },
  });
  return response.data;
};

export const getTimeline = async (duration = '24h'): Promise<{ timestamp: number; value: number }[]> => {
  const response = await api.get('/api/timeline', {
    params: { duration },
  });
  return response.data;
};

export default api;
