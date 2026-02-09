import { lazy, Suspense, useEffect } from 'react';
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

// Lazy-loaded heavy components for code splitting
const StatsOverview = lazy(() =>
  import('./components/Stats/StatsOverview').then((m) => ({ default: m.StatsOverview }))
);
const EditsTimelineChart = lazy(() =>
  import('./components/Stats/EditsTimelineChart').then((m) => ({ default: m.EditsTimelineChart }))
);
const LanguageDistributionChart = lazy(() =>
  import('./components/Stats/LanguageDistributionChart').then((m) => ({
    default: m.LanguageDistributionChart,
  }))
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

function App() {
  const activeTab = useAppStore((s) => s.activeTab);
  const darkMode = useAppStore((s) => s.darkMode);

  // Apply dark mode class on mount & changes
  useEffect(() => {
    document.documentElement.classList.toggle('dark', darkMode);
  }, [darkMode]);

  return (
    <div className="min-h-screen flex flex-col bg-gray-50 dark:bg-gray-950 transition-colors duration-200">
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
          </ErrorBoundary>
        </div>
      </main>

      <Footer />
    </div>
  );
}

/** Dashboard: full overview with all widgets */
function DashboardView() {
  return (
    <div className="space-y-6">
      {/* Stats - full width */}
      <section aria-label="Statistics overview">
        <ErrorBoundary>
          <Suspense fallback={<StatsOverviewSkeleton />}>
            <StatsOverview />
          </Suspense>
        </ErrorBoundary>
      </section>

      {/* Charts - 2 columns on desktop */}
      <section aria-label="Charts" className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <ErrorBoundary>
          <Suspense fallback={<ChartSkeleton />}>
            <EditsTimelineChart />
          </Suspense>
        </ErrorBoundary>
        <ErrorBoundary>
          <Suspense fallback={<ChartSkeleton />}>
            <LanguageDistributionChart />
          </Suspense>
        </ErrorBoundary>
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

      {/* Edit Wars - full width, collapsible */}
      <section aria-label="Edit wars">
        <ErrorBoundary>
          <Suspense fallback={<ChartSkeleton />}>
            <EditWarsList />
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
          <AlertsPanel />
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

/** Edit Wars view */
function EditWarsView() {
  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <ErrorBoundary>
        <Suspense fallback={<ChartSkeleton />}>
          <EditWarsList />
        </Suspense>
      </ErrorBoundary>
      <ErrorBoundary>
        <Suspense fallback={<ChartSkeleton />}>
          <HistoricalEditWars />
        </Suspense>
      </ErrorBoundary>
    </div>
  );
}

export default App;
