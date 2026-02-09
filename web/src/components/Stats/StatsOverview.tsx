import { useCallback, useRef } from 'react';
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

export function StatsOverview() {
  const updateStats = useAppStore((s) => s.updateStats);
  const previousStats = useRef<Stats | null>(null);

  const fetchFn = useCallback(async () => {
    const stats = await getStats();
    updateStats(stats);
    return stats;
  }, [updateStats]);

  const { data: stats, loading, error, refresh, lastUpdate } = usePollingData<Stats>({
    fetchFunction: fetchFn,
    interval: 5_000,
  });

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
            <div key={i} className="card animate-pulse border-l-4 border-gray-200 dark:border-gray-600">
              <div className="flex items-center space-x-3">
                <div className="h-10 w-10 bg-gray-200 dark:bg-gray-600 rounded-lg" />
                <div className="flex-1">
                  <div className="h-3 bg-gray-200 dark:bg-gray-600 rounded w-16 mb-2" />
                  <div className="h-6 bg-gray-200 dark:bg-gray-600 rounded w-12" />
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
      <div className="card text-center text-gray-500 dark:text-gray-400">
        <p>Failed to load stats</p>
        <button onClick={refresh} className="text-primary-600 hover:underline text-sm mt-1">
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
      color: 'text-blue-500',
      accentColor: 'border-blue-500',
      trend: trends.editsPerSecond,
    },
    {
      label: 'Edits Today',
      value: stats ? formatNumber(stats.edits_today) : '—',
      icon: Activity,
      color: 'text-green-500',
      accentColor: 'border-green-500',
      trend: trends.editsToday,
    },
    {
      label: 'Hot Pages',
      value: stats ? formatNumber(stats.hot_pages_count) : '—',
      icon: TrendingUp,
      color: 'text-orange-500',
      accentColor: 'border-orange-500',
      trend: trends.hotPages,
    },
    {
      label: 'Trending',
      value: stats ? formatNumber(stats.trending_count) : '—',
      icon: BarChart3,
      color: 'text-purple-500',
      accentColor: 'border-purple-500',
      trend: trends.trending,
    },
    {
      label: 'Active Alerts',
      value: stats ? formatNumber(stats.active_alerts) : '—',
      icon: AlertTriangle,
      color: 'text-red-500',
      accentColor: 'border-red-500',
      trend: trends.alerts,
    },
    {
      label: 'Top Language',
      value: stats?.top_language ?? '—',
      icon: Globe,
      color: 'text-indigo-500',
      accentColor: 'border-indigo-500',
    },
  ];

  return (
    <div className="space-y-2" role="region" aria-label="Statistics overview">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide">
          Overview
        </h2>
        <div className="flex items-center gap-2 text-xs text-gray-400">
          {lastUpdate && (
            <span>Updated {lastUpdate.toLocaleTimeString()}</span>
          )}
          <button
            onClick={refresh}
            className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
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
}
