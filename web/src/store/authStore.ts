import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { User, DigestPreferences } from '../types/user';
import api from '../utils/api';

interface AuthState {
  token: string | null;
  user: User | null;
  isLoading: boolean;
  error: string | null;

  // Actions
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  logout: () => void;
  fetchProfile: () => Promise<void>;
  updatePreferences: (prefs: DigestPreferences) => Promise<void>;
  updateWatchlist: (watchlist: string[]) => Promise<void>;
  clearError: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      token: null,
      user: null,
      isLoading: false,
      error: null,

      login: async (email, password) => {
        set({ isLoading: true, error: null });
        try {
          const res = await api.post('/api/auth/login', { email, password });
          const { token, user } = res.data;
          set({ token, user, isLoading: false });
        } catch (err: unknown) {
          const message = extractError(err);
          set({ isLoading: false, error: message });
          throw new Error(message);
        }
      },

      register: async (email, password) => {
        set({ isLoading: true, error: null });
        try {
          const res = await api.post('/api/auth/register', { email, password });
          const { token, user } = res.data;
          set({ token, user, isLoading: false });
        } catch (err: unknown) {
          const message = extractError(err);
          set({ isLoading: false, error: message });
          throw new Error(message);
        }
      },

      logout: () => {
        set({ token: null, user: null, error: null });
      },

      fetchProfile: async () => {
        const { token } = get();
        if (!token) return;
        try {
          const res = await api.get('/api/user/profile', {
            headers: { Authorization: `Bearer ${token}` },
          });
          set({ user: res.data });
        } catch {
          // Token expired or invalid — log out
          set({ token: null, user: null });
        }
      },

      updatePreferences: async (prefs) => {
        const { token, user } = get();
        if (!token) return;
        try {
          const res = await api.put('/api/user/preferences', prefs, {
            headers: { Authorization: `Bearer ${token}` },
          });
          if (user) {
            set({
              user: {
                ...user,
                ...res.data.preferences,
              },
            });
          }
        } catch (err: unknown) {
          const message = extractError(err);
          set({ error: message });
          throw new Error(message);
        }
      },

      updateWatchlist: async (watchlist) => {
        const { token, user } = get();
        if (!token) return;
        try {
          const res = await api.put(
            '/api/user/watchlist',
            { watchlist },
            { headers: { Authorization: `Bearer ${token}` } }
          );
          if (user) {
            set({ user: { ...user, watchlist: res.data.watchlist } });
          }
        } catch (err: unknown) {
          const message = extractError(err);
          set({ error: message });
          throw new Error(message);
        }
      },

      clearError: () => set({ error: null }),
    }),
    {
      name: 'wikisurge-auth',
      partialize: (state) => ({
        token: state.token,
        user: state.user,
      }),
    }
  )
);

function extractError(err: unknown): string {
  if (
    err &&
    typeof err === 'object' &&
    'response' in err &&
    (err as { response?: { data?: { error?: string } } }).response?.data?.error
  ) {
    return (err as { response: { data: { error: string } } }).response.data.error;
  }
  if (err instanceof Error) return err.message;
  return 'An unexpected error occurred';
}
