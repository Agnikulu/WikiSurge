import { useCallback } from 'react';
import { Activity, TrendingUp, AlertTriangle, BarChart3, Zap } from 'lucide-react';
import type { Stats } from '../../types';
import { getStats } from '../../utils/api';
import { useAPI } from '../../hooks/useAPI';
import { usePolling } from '../../hooks/usePolling';
import { useAppStore } from '../../store/appStore';
import { formatNumber } from '../../utils/formatting';
import { StatCard } from './StatCard';

export function StatsOverview() {
  const updateStats = useAppStore((s) => s.updateStats);

  const fetcher = useCallback(async () => {
    const stats = await getStats();
    return stats;
  }, []);

  const { data: stats, loading, error, refetch } = useAPI<Stats>({
    fetcher,
  });

  // Poll every 5 seconds
  usePolling({
    fetcher: async () => {
      const stats = await getStats();
      updateStats(stats);
    },
    interval: 5000,
  });

  if (loading && !stats) {
    return (
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="card animate-pulse">
            <div className="h-4 bg-gray-200 rounded w-24 mb-2" />
            <div className="h-8 bg-gray-200 rounded w-16" />
          </div>
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="card text-center text-gray-500">
        <p>Failed to load stats</p>
        <button onClick={refetch} className="text-primary-600 hover:underline text-sm mt-1">
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
      color: 'text-yellow-500',
    },
    {
      label: 'Edits Today',
      value: stats ? formatNumber(stats.edits_today) : '—',
      icon: Activity,
      color: 'text-blue-500',
    },
    {
      label: 'Hot Pages',
      value: stats ? formatNumber(stats.hot_pages_count) : '—',
      icon: TrendingUp,
      color: 'text-orange-500',
    },
    {
      label: 'Trending',
      value: stats ? formatNumber(stats.trending_count) : '—',
      icon: BarChart3,
      color: 'text-green-500',
    },
    {
      label: 'Active Alerts',
      value: stats ? formatNumber(stats.active_alerts) : '—',
      icon: AlertTriangle,
      color: 'text-red-500',
    },
  ];

  return (
    <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
      {statItems.map((item) => (
        <StatCard key={item.label} {...item} />
      ))}
    </div>
  );
}
