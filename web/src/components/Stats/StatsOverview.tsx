import { useCallback, useEffect, useRef, memo } from 'react';
import { Activity, TrendingUp, AlertTriangle, BarChart3, Zap, Globe, RefreshCw } from 'lucide-react';
import type { Stats } from '../../types';
import { getStats } from '../../utils/api';
import { usePollingData } from '../../hooks/usePolling';
import { useAppStore } from '../../store/appStore';
import { formatNumber } from '../../utils/formatting';
import { StatCard } from './StatCard';
import type { Trend } from './StatCard';

function computeTrend(current: number, previous: number): Trend | undefined {
  if (previous === 0) return undefined;
  const pct = ((current - previous) / previous) * 100;
  if (Math.abs(pct) < 0.1) return { direction: 'neutral', value: 0 };
  return { direction: pct > 0 ? 'up' : 'down', value: Math.abs(pct) };
}

export const StatsOverview = memo(function StatsOverview() {
  const updateStats = useAppStore((s) => s.updateStats);
  const setApiHealthy = useAppStore((s) => s.setApiHealthy);
  const previousStats = useRef<Stats | null>(null);

  const fetchFn = useCallback(async () => {
    const stats = await getStats();
    updateStats(stats);
    return stats;
  }, [updateStats]);

  const { data: stats, loading, error, refresh, lastUpdate } = usePollingData<Stats>({
    fetchFunction: fetchFn,
    interval: 10_000, // Increased from 5s to match backend cache
  });

  // Sync API health to global store
  useEffect(() => {
    setApiHealthy(!error);
  }, [error, setApiHealthy]);

  // Compute trends by comparing with previous snapshot
  const trends = {
    editsPerSecond: stats && previousStats.current
      ? computeTrend(stats.edits_per_second, previousStats.current.edits_per_second)
      : undefined,
    editsToday: stats && previousStats.current
      ? computeTrend(stats.edits_today, previousStats.current.edits_today)
      : undefined,
    hotPages: stats && previousStats.current
      ? computeTrend(stats.hot_pages_count, previousStats.current.hot_pages_count)
      : undefined,
    trending: stats && previousStats.current
      ? computeTrend(stats.trending_count, previousStats.current.trending_count)
      : undefined,
    alerts: stats && previousStats.current
      ? computeTrend(stats.active_alerts, previousStats.current.active_alerts)
      : undefined,
  };

  // Store current as previous for next cycle
  if (stats && stats !== previousStats.current) {
    previousStats.current = stats;
  }

  if (loading && !stats) {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="card animate-pulse" style={{ borderLeft: '3px solid rgba(0,255,136,0.15)' }}>
              <div className="flex items-center space-x-3">
                <div className="h-10 w-10 rounded-lg" style={{ background: 'rgba(0,255,136,0.06)' }} />
                <div className="flex-1">
                  <div className="h-3 rounded w-16 mb-2" style={{ background: 'rgba(0,255,136,0.1)' }} />
                  <div className="h-6 rounded w-12" style={{ background: 'rgba(0,255,136,0.1)' }} />
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="card text-center" style={{ color: 'rgba(0,255,136,0.4)' }}>
        <p>Failed to load stats</p>
        <button onClick={refresh} className="hover:underline text-sm mt-1" style={{ color: '#00ff88' }}>
          Retry
        </button>
      </div>
    );
  }

  const statItems = [
    {
      label: 'Edits/sec',
      value: stats?.edits_per_second?.toFixed(1) ?? '—',
      icon: Zap,
      color: '#00ff88',
      trend: trends.editsPerSecond,
    },
    {
      label: 'Edits Today',
      value: stats ? formatNumber(stats.edits_today) : '—',
      icon: Activity,
      color: '#00ff88',
      trend: trends.editsToday,
    },
    {
      label: 'Hot Pages',
      value: stats ? formatNumber(stats.hot_pages_count) : '—',
      icon: TrendingUp,
      color: '#ffaa00',
      trend: trends.hotPages,
    },
    {
      label: 'Trending',
      value: stats ? formatNumber(stats.trending_count) : '—',
      icon: BarChart3,
      color: '#00ddff',
      trend: trends.trending,
    },
    {
      label: 'Active Alerts',
      value: stats ? formatNumber(stats.active_alerts) : '—',
      icon: AlertTriangle,
      color: '#ff4444',
      trend: trends.alerts,
    },
    {
      label: 'Top Language',
      value: stats?.top_language ?? '—',
      icon: Globe,
      color: '#00ff88',
    },
  ];

  return (
    <div className="space-y-2" role="region" aria-label="Statistics overview">
      <div className="flex items-center justify-between">
        <h2 className="text-[10px] font-mono font-medium uppercase tracking-widest" style={{ color: 'rgba(0,255,136,0.4)' }}>
          SYSTEM OVERVIEW
        </h2>
        <div className="flex items-center gap-2 text-[10px] font-mono" style={{ color: 'rgba(0,255,136,0.3)' }}>
          {lastUpdate && (
            <span>Updated {lastUpdate.toLocaleTimeString()}</span>
          )}
          <button
            onClick={refresh}
            className="p-1 rounded transition-colors"
            style={{ color: 'rgba(0,255,136,0.3)' }}
            aria-label="Refresh stats"
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
        {statItems.map((item) => (
          <StatCard key={item.label} {...item} />
        ))}
      </div>
    </div>
  );
});
