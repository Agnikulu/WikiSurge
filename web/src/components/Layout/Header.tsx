import { Activity, Wifi, WifiOff } from 'lucide-react';
import { useAppStore } from '../../store/appStore';

export function Header() {
  const activeTab = useAppStore((s) => s.activeTab);
  const setActiveTab = useAppStore((s) => s.setActiveTab);

  const tabs = [
    { id: 'dashboard', label: 'Dashboard' },
    { id: 'trending', label: 'Trending' },
    { id: 'alerts', label: 'Alerts' },
    { id: 'search', label: 'Search' },
    { id: 'edit-wars', label: 'Edit Wars' },
  ];

  return (
    <header className="bg-white border-b border-gray-200 sticky top-0 z-50">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex items-center justify-between h-16">
          {/* Logo & Title */}
          <div className="flex items-center space-x-3">
            <Activity className="h-8 w-8 text-primary-600" />
            <div>
              <h1 className="text-xl font-bold text-gray-900">WikiSurge</h1>
              <p className="text-xs text-gray-500">Real-time Wikipedia Analytics</p>
            </div>
          </div>

          {/* Navigation Tabs */}
          <nav className="hidden md:flex space-x-1">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                className={`px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                  activeTab === tab.id
                    ? 'bg-primary-50 text-primary-700'
                    : 'text-gray-500 hover:text-gray-700 hover:bg-gray-50'
                }`}
              >
                {tab.label}
              </button>
            ))}
          </nav>

          {/* Connection Status */}
          <ConnectionStatus />
        </div>
      </div>
    </header>
  );
}

function ConnectionStatus() {
  // This will be connected to the actual WebSocket status later
  const connected = false;

  return (
    <div className="flex items-center space-x-2 text-sm">
      {connected ? (
        <>
          <Wifi className="h-4 w-4 text-green-500" />
          <span className="text-green-600 hidden sm:inline">Live</span>
        </>
      ) : (
        <>
          <WifiOff className="h-4 w-4 text-gray-400" />
          <span className="text-gray-400 hidden sm:inline">Offline</span>
        </>
      )}
    </div>
  );
}
