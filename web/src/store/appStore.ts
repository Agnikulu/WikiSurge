import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { TrendingPage, Stats, FilterState } from '../types';

interface AppState {
  // Filters
  filters: FilterState;

  // UI state
  selectedPage: string | null;
  activeTab: string;
  darkMode: boolean;

  // Connection state
  wsConnected: boolean;
  apiHealthy: boolean;

  // Cached data
  trending: TrendingPage[];
  stats: Stats | null;

  // Actions
  setFilters: (filters: Partial<FilterState>) => void;
  setSelectedPage: (page: string | null) => void;
  setActiveTab: (tab: string) => void;
  toggleDarkMode: () => void;
  setWsConnected: (connected: boolean) => void;
  setApiHealthy: (healthy: boolean) => void;
  updateTrending: (trending: TrendingPage[]) => void;
  updateStats: (stats: Stats) => void;
  resetFilters: () => void;
}

const DEFAULT_FILTERS: FilterState = {
  languages: [],
  excludeBots: false,
  minByteChange: 0,
};

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      // Initial state
      filters: { ...DEFAULT_FILTERS },
      selectedPage: null,
      activeTab: 'dashboard',
      darkMode: false,
      wsConnected: false,
      apiHealthy: true,
      trending: [],
      stats: null,

      // Actions
      setFilters: (filters) =>
        set((state) => ({
          filters: { ...state.filters, ...filters },
        })),

      setSelectedPage: (page) => set({ selectedPage: page }),

      setActiveTab: (tab) => set({ activeTab: tab }),

      toggleDarkMode: () =>
        set((state) => ({ darkMode: !state.darkMode })),

      setWsConnected: (connected) => set({ wsConnected: connected }),

      setApiHealthy: (healthy) => set({ apiHealthy: healthy }),

      updateTrending: (trending) => set({ trending }),

      updateStats: (stats) => set({ stats }),

      resetFilters: () => set({ filters: { ...DEFAULT_FILTERS } }),
    }),
    {
      name: 'wikisurge-settings',
      partialize: (state) => ({
        filters: state.filters,
        activeTab: state.activeTab,
        darkMode: state.darkMode,
      }),
    }
  )
);
