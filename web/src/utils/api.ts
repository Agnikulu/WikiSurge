import axios from 'axios';
import type { TrendingPage, Edit, Alert, Stats, EditWar, SearchResult } from '../types';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

const api = axios.create({
  baseURL: API_BASE_URL,
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
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

export const searchEdits = async (query: string, limit = 50): Promise<SearchResult> => {
  const response = await api.get('/api/search', {
    params: { q: query, limit },
  });
  return response.data;
};

export const getAlerts = async (limit = 20): Promise<Alert[]> => {
  const response = await api.get('/api/alerts', { params: { limit } });
  return response.data;
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

export default api;
