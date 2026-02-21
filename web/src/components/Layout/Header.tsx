import { Activity, Wifi, WifiOff, Menu, X, Server, ServerOff, User, LogIn } from 'lucide-react';
import { useAppStore } from '../../store/appStore';
import { useAuthStore } from '../../store/authStore';
import { useState, useCallback } from 'react';

export function Header() {
  const activeTab = useAppStore((s) => s.activeTab);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const wsConnected = useAppStore((s) => s.wsConnected);
  const apiHealthy = useAppStore((s) => s.apiHealthy);
  const user = useAuthStore((s) => s.user);
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

  return (
    <header className="sticky top-0 z-50" style={{ background: '#0d1525', borderBottom: '1px solid rgba(0,255,136,0.12)' }} role="banner">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex items-center justify-between h-14">
          {/* Logo & Title */}
          <div className="flex items-center space-x-3 min-w-0">
            <Activity className="h-7 w-7 flex-shrink-0" style={{ color: '#00ff88' }} aria-hidden="true" />
            <div className="min-w-0">
              <h1 className="text-lg font-bold font-mono tracking-wider" style={{ color: '#00ff88' }}>WIKISURGE</h1>
              <p className="text-[10px] font-mono hidden sm:block" style={{ color: 'rgba(0,255,136,0.35)' }}>REAL-TIME INTELLIGENCE</p>
            </div>
          </div>

          {/* Desktop Navigation Tabs */}
          <nav className="hidden md:flex space-x-1" role="navigation" aria-label="Main navigation">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => handleTabClick(tab.id)}
                aria-current={activeTab === tab.id ? 'page' : undefined}
                className="px-3 py-1.5 rounded text-xs font-mono font-medium transition-all"
                style={{
                  background: activeTab === tab.id ? 'rgba(0,255,136,0.12)' : 'transparent',
                  color: activeTab === tab.id ? '#00ff88' : 'rgba(0,255,136,0.4)',
                  borderBottom: activeTab === tab.id ? '2px solid #00ff88' : '2px solid transparent',
                }}
              >
                {tab.label.toUpperCase()}
              </button>
            ))}
          </nav>

          {/* Right Section */}
          <div className="flex items-center space-x-3">
            {/* Connection Status Indicators */}
            <div className="hidden sm:flex items-center space-x-3 text-xs font-mono" aria-live="polite">
              <span className="flex items-center space-x-1.5" title={wsConnected ? 'WebSocket connected' : 'WebSocket disconnected'}>
                {wsConnected ? (
                  <Wifi className="h-3.5 w-3.5" style={{ color: '#00ff88' }} aria-hidden="true" />
                ) : (
                  <WifiOff className="h-3.5 w-3.5" style={{ color: '#ff4444' }} aria-hidden="true" />
                )}
                <span style={{ color: wsConnected ? '#00ff88' : '#ff4444' }}>
                  {wsConnected ? 'LIVE' : 'OFFLINE'}
                </span>
                {wsConnected && (
                  <span className="inline-block w-1.5 h-1.5 rounded-full animate-pulse" style={{ backgroundColor: '#00ff88', boxShadow: '0 0 6px #00ff88' }} />
                )}
              </span>
              <span className="flex items-center space-x-1.5" title={apiHealthy ? 'API healthy' : 'API degraded'}>
                {apiHealthy ? (
                  <Server className="h-3.5 w-3.5" style={{ color: '#00ff88' }} aria-hidden="true" />
                ) : (
                  <ServerOff className="h-3.5 w-3.5" style={{ color: '#ffaa00' }} aria-hidden="true" />
                )}
                <span style={{ color: apiHealthy ? '#00ff88' : '#ffaa00' }}>
                  {apiHealthy ? 'API' : 'DEGRADED'}
                </span>
              </span>
            </div>

            {/* User / Settings Button */}
            <button
              onClick={() => handleTabClick('settings')}
              className="flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-mono transition-all"
              style={{
                background: activeTab === 'settings' ? 'rgba(0,255,136,0.15)' : 'rgba(0,255,136,0.05)',
                border: `1px solid ${activeTab === 'settings' ? 'rgba(0,255,136,0.3)' : 'rgba(0,255,136,0.1)'}`,
                color: activeTab === 'settings' ? '#00ff88' : 'rgba(0,255,136,0.5)',
              }}
              title={user ? `Settings (${user.email})` : 'Sign in'}
            >
              {user ? (
                <>
                  <User className="h-3.5 w-3.5" />
                  <span className="hidden lg:inline max-w-[80px] truncate">{user.email.split('@')[0]}</span>
                </>
              ) : (
                <>
                  <LogIn className="h-3.5 w-3.5" />
                  <span className="hidden lg:inline">SIGN IN</span>
                </>
              )}
            </button>

            {/* Mobile Menu Button */}
            <button
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
              aria-label="Toggle menu"
              aria-expanded={mobileMenuOpen}
              className="md:hidden p-2 rounded transition-colors"
              style={{ color: 'rgba(0,255,136,0.6)' }}
            >
              {mobileMenuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
            </button>
          </div>
        </div>
      </div>

      {/* Mobile Navigation Menu */}
      {mobileMenuOpen && (
        <nav className="md:hidden animate-slide-down" style={{ background: '#0d1525', borderTop: '1px solid rgba(0,255,136,0.1)' }} role="navigation" aria-label="Mobile navigation">
          <div className="px-4 py-2 space-y-1">
            {tabs.map((tab) => (
              <button
                key={tab.id}
                onClick={() => handleTabClick(tab.id)}
                aria-current={activeTab === tab.id ? 'page' : undefined}
                className="block w-full text-left px-3 py-2 rounded text-xs font-mono font-medium transition-colors"
                style={{
                  background: activeTab === tab.id ? 'rgba(0,255,136,0.1)' : 'transparent',
                  color: activeTab === tab.id ? '#00ff88' : 'rgba(0,255,136,0.4)',
                }}
              >
                {tab.label.toUpperCase()}
              </button>
            ))}
            {/* Mobile connection status */}
            <div className="sm:hidden flex items-center space-x-4 px-3 py-2 text-[10px] font-mono mt-1 pt-2" style={{ borderTop: '1px solid rgba(0,255,136,0.08)', color: 'rgba(0,255,136,0.4)' }}>
              <span className="flex items-center space-x-1">
                {wsConnected ? <Wifi className="h-3.5 w-3.5" style={{ color: '#00ff88' }} /> : <WifiOff className="h-3.5 w-3.5" style={{ color: '#ff4444' }} />}
                <span>{wsConnected ? 'LIVE' : 'OFFLINE'}</span>
              </span>
              <span className="flex items-center space-x-1">
                {apiHealthy ? <Server className="h-3.5 w-3.5" style={{ color: '#00ff88' }} /> : <ServerOff className="h-3.5 w-3.5" style={{ color: '#ffaa00' }} />}
                <span>{apiHealthy ? 'API OK' : 'DEGRADED'}</span>
              </span>
            </div>
          </div>
        </nav>
      )}
    </header>
  );
}
