import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { User, DigestPreferences, AdminUserListResponse } from '../types/user';
import api from '../utils/api';

interface AuthState {
  token: string | null;
  user: User | null;
  isLoading: boolean;
  error: string | null;
  adminUsers: User[] | null;
  adminUsersLoading: boolean;

  // Actions
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  logout: () => void;
  fetchProfile: () => Promise<void>;
  updatePreferences: (prefs: DigestPreferences) => Promise<void>;
  updateWatchlist: (watchlist: string[]) => Promise<void>;
  fetchAdminUsers: () => Promise<void>;
  deleteAdminUser: (userId: string) => Promise<void>;
  clearError: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      token: null,
      user: null,
      isLoading: false,
      error: null,
      adminUsers: null,
      adminUsersLoading: false,

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
        set({ token: null, user: null, error: null, adminUsers: null });
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

      fetchAdminUsers: async () => {
        const { token, user } = get();
        if (!token || !user?.is_admin) return;
        set({ adminUsersLoading: true });
        try {
          const res = await api.get<AdminUserListResponse>('/api/admin/users', {
            headers: { Authorization: `Bearer ${token}` },
          });
          set({ adminUsers: res.data.users, adminUsersLoading: false });
        } catch (err: unknown) {
          const message = extractError(err);
          set({ adminUsersLoading: false, error: message });
        }
      },

      deleteAdminUser: async (userId: string) => {
        const { token, user, adminUsers } = get();
        if (!token || !user?.is_admin) return;
        try {
          await api.delete(`/api/admin/users/${userId}`, {
            headers: { Authorization: `Bearer ${token}` },
          });
          // Remove from local state immediately
          if (adminUsers) {
            set({ adminUsers: adminUsers.filter((u) => u.id !== userId) });
          }
        } catch (err: unknown) {
          const message = extractError(err);
          set({ error: message });
          throw new Error(message);
        }
      },
    }),
    {
      name: 'wikisurge-auth',
      version: 2, // Bump when User shape changes — auto-clears stale data
      partialize: (state) => ({
        token: state.token,
        user: state.user,
      }),
      migrate: (persistedState: unknown, version: number) => {
        // Version < 2: user object was missing is_admin and other fields
        // Keep token so user stays logged in, but null out stale user so
        // fetchProfile refreshes it from the backend on next load
        if (version < 2) {
          const state = persistedState as { token?: string | null; user?: unknown };
          return { token: state?.token ?? null, user: null };
        }
        return persistedState as { token: string | null; user: null };
      },
    }
  )
);

function extractError(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const res = (err as { response?: { data?: unknown } }).response;
    const data = res?.data;
    if (data && typeof data === 'object') {
      const d = data as Record<string, unknown>;
      // Backend returns { error: { message, code, request_id } }
      if (d.error && typeof d.error === 'object' && 'message' in (d.error as object)) {
        return String((d.error as Record<string, unknown>).message);
      }
      // Fallback: { error: "string" }
      if (typeof d.error === 'string') return d.error;
      // Fallback: { message: "string" }
      if (typeof d.message === 'string') return d.message;
    }
  }
  if (err instanceof Error) return err.message;
  return 'An unexpected error occurred';
}
