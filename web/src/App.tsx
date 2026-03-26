import { lazy, Suspense, useEffect, useState, useCallback } from 'react';
import { Header } from './components/Layout/Header';
import { Footer } from './components/Layout/Footer';
import { ErrorBoundary } from './components/ui/ErrorBoundary';
import { SkipLink } from './components/ui/SkipLink';
import {
  StatsOverviewSkeleton,
  ChartSkeleton,
  TrendingListSkeleton,
  SearchSkeleton,
} from './components/ui/Skeleton';
import { useAppStore } from './store/appStore';
import { LanguageDistributionChart } from './components/Stats/LanguageDistributionChart';
import { MapSkeleton } from './components/Map/MapSkeleton';
import { getGeoActivity } from './utils/api';
import type { GeoWar } from './types';

// Lazy-loaded heavy components for code splitting
const GlobalActivityMap = lazy(() =>
  import('./components/Map/GlobalActivityMap').then((m) => ({ default: m.GlobalActivityMap }))
);
const ConflictSpotlight = lazy(() =>
  import('./components/EditWars/ConflictSpotlight').then((m) => ({ default: m.ConflictSpotlight }))
);
const StatsOverview = lazy(() =>
  import('./components/Stats/StatsOverview').then((m) => ({ default: m.StatsOverview }))
);
const EditsTimelineChart = lazy(() =>
  import('./components/Stats/EditsTimelineChart').then((m) => ({ default: m.EditsTimelineChart }))
);
const AlertsPanel = lazy(() =>
  import('./components/Alerts/AlertsPanel').then((m) => ({ default: m.AlertsPanel }))
);
const TrendingList = lazy(() =>
  import('./components/Trending/TrendingList').then((m) => ({ default: m.TrendingList }))
);
const SearchInterface = lazy(() =>
  import('./components/Search/SearchInterface').then((m) => ({ default: m.SearchInterface }))
);
const LiveFeed = lazy(() =>
  import('./components/LiveFeed/LiveFeed').then((m) => ({ default: m.LiveFeed }))
);
const EditWarsList = lazy(() =>
  import('./components/EditWars/EditWarsList').then((m) => ({ default: m.EditWarsList }))
);
const HistoricalEditWars = lazy(() =>
  import('./components/EditWars/HistoricalEditWars').then((m) => ({
    default: m.HistoricalEditWars,
  }))
);
import { LoginForm } from './components/Auth/LoginForm';
import { SettingsPanel } from './components/Settings/SettingsPanel';
import { useAuthStore } from './store/authStore';

function App() {
  const activeTab = useAppStore((s) => s.activeTab);
  const darkMode = useAppStore((s) => s.darkMode);
  const fetchProfile = useAuthStore((s) => s.fetchProfile);
  const token = useAuthStore((s) => s.token);

  // Apply dark mode class on mount & changes
  useEffect(() => {
    document.documentElement.classList.toggle('dark', darkMode);
  }, [darkMode]);

  // On mount: refresh user profile from backend to overwrite stale localStorage
  useEffect(() => {
    if (token) {
      fetchProfile();
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="min-h-screen flex flex-col bg-[#0a0f1a] transition-colors duration-200">
      <SkipLink />
      <Header />

      <main id="main-content" className="flex-1" role="main" tabIndex={-1}>
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
          <ErrorBoundary>
            {activeTab === 'dashboard' && <DashboardView />}
            {activeTab === 'trending' && <TrendingView />}
            {activeTab === 'alerts' && <AlertsView />}
            {activeTab === 'search' && <SearchView />}
            {activeTab === 'edit-wars' && <EditWarsView />}
            {activeTab === 'settings' && <SettingsView />}
          </ErrorBoundary>
        </div>
      </main>

      <Footer />
    </div>
  );
}

/** Dashboard: full overview with all widgets */
function DashboardView() {
  const setActiveEditWarsCount = useAppStore((s) => s.setActiveEditWarsCount);
  const [spotlightWars, setSpotlightWars] = useState<GeoWar[]>([]);

  // Fetch geo data for spotlight (map fetches its own)
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const data = await getGeoActivity();
        if (!cancelled) {
          setSpotlightWars(data.wars);
          setActiveEditWarsCount(data.wars.filter((w) => w.active).length);
        }
      } catch { /* silently ignore — map has its own fetch */ }
    };
    load();
    const interval = setInterval(load, 15_000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [setActiveEditWarsCount]);

  const handleWarClick = useCallback((war: GeoWar) => {
    const warId = `edit-war-${war.page_title.replace(/[^a-zA-Z0-9]+/g, '-').toLowerCase()}`;
    useAppStore.getState().setPendingScrollToWar(warId);
    useAppStore.getState().setActiveTab('edit-wars');
  }, []);

  return (
    <div className="space-y-6">
      {/* Stats - full width (System Overview) */}
      <section aria-label="Statistics overview">
        <ErrorBoundary>
          <Suspense fallback={<StatsOverviewSkeleton />}>
            <StatsOverview />
          </Suspense>
        </ErrorBoundary>
      </section>

      {/* Map Hero - full width */}
      <section aria-label="Global activity map">
        <ErrorBoundary>
          <Suspense fallback={<MapSkeleton />}>
            <GlobalActivityMap height={400} onWarClick={handleWarClick} />
          </Suspense>
        </ErrorBoundary>
      </section>

      {/* Conflict Spotlight - full width */}
      {spotlightWars.length > 0 && (
        <section aria-label="Conflict spotlight">
          <ErrorBoundary>
            <Suspense fallback={<ChartSkeleton />}>
              <ConflictSpotlight wars={spotlightWars} onViewDetails={handleWarClick} />
            </Suspense>
          </ErrorBoundary>
        </section>
      )}

      {/* Charts - 2 columns on desktop */}
      <section aria-label="Charts" className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Suspense fallback={<ChartSkeleton />}>
          <EditsTimelineChart />
        </Suspense>
        <LanguageDistributionChart />
      </section>

      {/* Alerts + Trending - 2 columns */}
      <section aria-label="Alerts and trending" className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <ErrorBoundary>
          <Suspense fallback={<TrendingListSkeleton />}>
            <AlertsPanel />
          </Suspense>
        </ErrorBoundary>
        <ErrorBoundary>
          <Suspense fallback={<TrendingListSkeleton />}>
            <TrendingList />
          </Suspense>
        </ErrorBoundary>
      </section>

      {/* Search + Live Feed - 2 columns */}
      <section aria-label="Search and live feed" className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <ErrorBoundary>
          <Suspense fallback={<SearchSkeleton />}>
            <SearchInterface />
          </Suspense>
        </ErrorBoundary>
        <ErrorBoundary>
          <Suspense fallback={<TrendingListSkeleton />}>
            <LiveFeed />
          </Suspense>
        </ErrorBoundary>
      </section>
    </div>
  );
}

/** Trending-only view */
function TrendingView() {
  return (
    <div className="max-w-3xl mx-auto">
      <ErrorBoundary>
        <Suspense fallback={<TrendingListSkeleton />}>
          <TrendingList />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
}

/** Alerts-only view */
function AlertsView() {
  return (
    <div className="max-w-3xl mx-auto">
      <ErrorBoundary>
        <Suspense fallback={<TrendingListSkeleton />}>
          <AlertsPanel showHistorical />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
}

/** Search view */
function SearchView() {
  return (
    <div className="max-w-4xl mx-auto">
      <ErrorBoundary>
        <Suspense fallback={<SearchSkeleton />}>
          <SearchInterface />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
}

/** Edit Wars "War Room" view */
function EditWarsView() {
  const pendingWar = useAppStore((s) => s.pendingScrollToWar);
  const setPendingScrollToWar = useAppStore((s) => s.setPendingScrollToWar);

  useEffect(() => {
    if (!pendingWar) return;
    // Small delay to let the list render
    const timer = setTimeout(() => {
      const el = document.getElementById(pendingWar);
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
        el.classList.add('ring-2', 'ring-cyan-400');
        setTimeout(() => el.classList.remove('ring-2', 'ring-cyan-400'), 2000);
      }
      setPendingScrollToWar(null);
    }, 400);
    return () => clearTimeout(timer);
  }, [pendingWar, setPendingScrollToWar]);

  return (
    <div className="space-y-6">
      {/* Active wars list */}
      <ErrorBoundary>
        <Suspense fallback={<ChartSkeleton />}>
          <EditWarsList />
        </Suspense>
      </ErrorBoundary>

      {/* Historical wars */}
      <ErrorBoundary>
        <Suspense fallback={<ChartSkeleton />}>
          <HistoricalEditWars />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
}

/** Settings / Auth view */
function SettingsView() {
  const user = useAuthStore((s) => s.user);
  return user ? <SettingsPanel /> : <LoginForm />;
}

export default App;
