import { Header } from './components/Layout/Header';
import { Footer } from './components/Layout/Footer';
import { StatsOverview } from './components/Stats/StatsOverview';
import { EditsTimelineChart } from './components/Stats/EditsTimelineChart';
import { LanguageDistributionChart } from './components/Stats/LanguageDistributionChart';
import { AlertsPanel } from './components/Alerts/AlertsPanel';
import { TrendingList } from './components/Trending/TrendingList';
import { SearchInterface } from './components/Search/SearchInterface';
import { LiveFeed } from './components/LiveFeed/LiveFeed';
import { EditWarsList } from './components/EditWars/EditWarsList';
import { useAppStore } from './store/appStore';

function App() {
  const activeTab = useAppStore((s) => s.activeTab);

  return (
    <div className="min-h-screen flex flex-col bg-gray-50">
      <Header />

      <main className="flex-1">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
          {activeTab === 'dashboard' && <DashboardView />}
          {activeTab === 'trending' && <TrendingView />}
          {activeTab === 'alerts' && <AlertsView />}
          {activeTab === 'search' && <SearchView />}
          {activeTab === 'edit-wars' && <EditWarsView />}
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
      <StatsOverview />

      {/* Charts - 2 columns */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <EditsTimelineChart />
        <LanguageDistributionChart />
      </div>

      {/* Alerts + Trending - 2 columns */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <AlertsPanel />
        <TrendingList />
      </div>

      {/* Search + Live Feed - 2 columns */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <SearchInterface />
        <LiveFeed />
      </div>

      {/* Edit Wars - full width */}
      <EditWarsList />
    </div>
  );
}

/** Trending-only view */
function TrendingView() {
  return (
    <div className="max-w-3xl mx-auto">
      <TrendingList />
    </div>
  );
}

/** Alerts-only view */
function AlertsView() {
  return (
    <div className="max-w-3xl mx-auto">
      <AlertsPanel />
    </div>
  );
}

/** Search view */
function SearchView() {
  return (
    <div className="max-w-4xl mx-auto">
      <SearchInterface />
    </div>
  );
}

/** Edit Wars view */
function EditWarsView() {
  return (
    <div className="max-w-4xl mx-auto">
      <EditWarsList />
    </div>
  );
}

export default App;
