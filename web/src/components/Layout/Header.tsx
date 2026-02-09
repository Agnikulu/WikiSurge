import { Activity, Wifi, WifiOff, Moon, Sun, Menu, X, Server, ServerOff } from 'lucide-react';
import { useAppStore } from '../../store/appStore';
import { useState, useEffect, useCallback } from 'react';

export function Header() {
  const activeTab = useAppStore((s) => s.activeTab);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const darkMode = useAppStore((s) => s.darkMode);
  const toggleDarkMode = useAppStore((s) => s.toggleDarkMode);
  const wsConnected = useAppStore((s) => s.wsConnected);
  const apiHealthy = useAppStore((s) => s.apiHealthy);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  const tabs = [
    { id: 'dashboard', label: 'Dashboard' },
    { id: 'trending', label: 'Trending' },
    { id: 'alerts', label: 'Alerts' },
    { id: 'search', label: 'Search' },
    { id: 'edit-wars', label: 'Edit Wars' },
  ] as const;

  const handleTabClick = useCallback((id: string) => {
    setActiveTab(id);
    setMobileMenuOpen(false);
  }, [setActiveTab]);

  // Sync dark mode class on <html>
  useEffect(() => {
    document.documentElement.classList.toggle('dark', darkMode);
  }, [darkMode]);

  return (
    <header className="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700 sticky top-0 z-50 shadow-sm" role="banner">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex items-center justify-between h-16">
          {/* Logo & Title */}
          <div className="flex items-center space-x-3 min-w-0">
            <Activity className="h-8 w-8 text-primary-600 dark:text-primary-400 flex-shrink-0" aria-hidden="true" />
            <div className="min-w-0">
              <h1 className="text-xl font-bold text-gray-900 dark:text-white truncate">WikiSurge</h1>
              <p className="text-xs text-gray-500 dark:text-gray-400 hidden sm:block">Analyzing global edit activity</p>
            </div>
          </div>

          {/* Desktop Navigation Tabs */}
          <nav className="hidden md:flex space-x-1" role="navigation" aria-label="Main navigation">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => handleTabClick(tab.id)}
                aria-current={activeTab === tab.id ? 'page' : undefined}
                className={`px-3 py-2 rounded-md text-sm font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 dark:focus:ring-offset-gray-900 ${
                  activeTab === tab.id
                    ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/50 dark:text-primary-300'
                    : 'text-gray-500 hover:text-gray-700 hover:bg-gray-50 dark:text-gray-400 dark:hover:text-gray-200 dark:hover:bg-gray-800'
                }`}
              >
                {tab.label}
              </button>
            ))}
          </nav>

          {/* Right Section */}
          <div className="flex items-center space-x-3">
            {/* Connection Status Indicators */}
            <div className="hidden sm:flex items-center space-x-3 text-sm" aria-live="polite">
              <span className="flex items-center space-x-1" title={wsConnected ? 'WebSocket connected' : 'WebSocket disconnected'}>
                {wsConnected ? (
                  <Wifi className="h-4 w-4 text-green-500" aria-hidden="true" />
                ) : (
                  <WifiOff className="h-4 w-4 text-red-400" aria-hidden="true" />
                )}
                <span className={`text-xs ${wsConnected ? 'text-green-600 dark:text-green-400' : 'text-red-400'}`}>
                  {wsConnected ? 'Live' : 'Offline'}
                </span>
              </span>
              <span className="flex items-center space-x-1" title={apiHealthy ? 'API healthy' : 'API degraded'}>
                {apiHealthy ? (
                  <Server className="h-4 w-4 text-green-500" aria-hidden="true" />
                ) : (
                  <ServerOff className="h-4 w-4 text-yellow-500" aria-hidden="true" />
                )}
                <span className={`text-xs ${apiHealthy ? 'text-green-600 dark:text-green-400' : 'text-yellow-500'}`}>
                  {apiHealthy ? 'API' : 'Degraded'}
                </span>
              </span>
            </div>

            {/* Dark Mode Toggle */}
            <button
              onClick={toggleDarkMode}
              aria-label={darkMode ? 'Switch to light mode' : 'Switch to dark mode'}
              className="p-2 rounded-md text-gray-500 hover:text-gray-700 hover:bg-gray-100 dark:text-gray-400 dark:hover:text-gray-200 dark:hover:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-primary-500 transition-colors"
            >
              {darkMode ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
            </button>

            {/* Mobile Menu Button */}
            <button
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
              aria-label="Toggle menu"
              aria-expanded={mobileMenuOpen}
              className="md:hidden p-2 rounded-md text-gray-500 hover:text-gray-700 hover:bg-gray-100 dark:text-gray-400 dark:hover:text-gray-200 dark:hover:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-primary-500"
            >
              {mobileMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
            </button>
          </div>
        </div>
      </div>

      {/* Mobile Navigation Menu */}
      {mobileMenuOpen && (
        <nav className="md:hidden border-t border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 animate-slide-down" role="navigation" aria-label="Mobile navigation">
          <div className="px-4 py-2 space-y-1">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => handleTabClick(tab.id)}
                aria-current={activeTab === tab.id ? 'page' : undefined}
                className={`block w-full text-left px-3 py-2 rounded-md text-sm font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500 ${
                  activeTab === tab.id
                    ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/50 dark:text-primary-300'
                    : 'text-gray-600 hover:bg-gray-50 dark:text-gray-400 dark:hover:bg-gray-800'
                }`}
              >
                {tab.label}
              </button>
            ))}
            {/* Mobile connection status */}
            <div className="sm:hidden flex items-center space-x-4 px-3 py-2 text-xs text-gray-500 dark:text-gray-400 border-t border-gray-100 dark:border-gray-800 mt-1 pt-2">
              <span className="flex items-center space-x-1">
                {wsConnected ? <Wifi className="h-3.5 w-3.5 text-green-500" /> : <WifiOff className="h-3.5 w-3.5 text-red-400" />}
                <span>{wsConnected ? 'Live' : 'Offline'}</span>
              </span>
              <span className="flex items-center space-x-1">
                {apiHealthy ? <Server className="h-3.5 w-3.5 text-green-500" /> : <ServerOff className="h-3.5 w-3.5 text-yellow-500" />}
                <span>{apiHealthy ? 'API OK' : 'Degraded'}</span>
              </span>
            </div>
          </div>
        </nav>
      )}
    </header>
  );
}
