import { memo } from 'react';
import { TrendingUp, TrendingDown } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

export interface Trend {
  direction: 'up' | 'down' | 'neutral';
  value: number; // percentage change
}

interface StatCardProps {
  label: string;
  value: string;
  icon: LucideIcon;
  color: string;
  trend?: Trend;
  accentColor?: string; // tailwind border-color class, e.g. 'border-blue-500'
}

export const StatCard = memo(function StatCard({
  label,
  value,
  icon: Icon,
  color,
  trend,
  accentColor,
}: StatCardProps) {
  const borderClass = accentColor ?? 'border-gray-200';

  return (
    <div
      className={`card border-l-4 ${borderClass} flex items-center space-x-3
        transition-all duration-200 hover:shadow-md hover:-translate-y-0.5 cursor-default`}
    >
      <div className={`p-2.5 rounded-lg bg-gray-50 dark:bg-gray-700 ${color}`}>
        <Icon className="h-5 w-5" />
      </div>

      <div className="flex-1 min-w-0">
        <p className="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide truncate">
          {label}
        </p>
        <div className="flex items-baseline gap-2">
          <p className="text-2xl font-bold text-gray-900 dark:text-white tabular-nums">{value}</p>

          {trend && trend.direction !== 'neutral' && (
            <span
              className={`inline-flex items-center text-xs font-medium gap-0.5
                ${trend.direction === 'up' ? 'text-green-600' : 'text-red-600'}`}
            >
              {trend.direction === 'up' ? (
                <TrendingUp className="h-3 w-3" />
              ) : (
                <TrendingDown className="h-3 w-3" />
              )}
              {trend.value.toFixed(1)}%
            </span>
          )}
        </div>
      </div>
    </div>
  );
});
