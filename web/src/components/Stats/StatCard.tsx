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
}: StatCardProps) {
  return (
    <div
      className="card flex items-center space-x-3 transition-all duration-200 hover:-translate-y-0.5 cursor-default"
      style={{ borderLeft: '3px solid rgba(0,255,136,0.3)' }}
    >
      <div className="p-2.5 rounded-lg" style={{ background: 'rgba(0,255,136,0.06)' }}>
        <Icon className={`h-5 w-5 ${color}`} style={color.startsWith('text-') ? undefined : { color }} />
      </div>

      <div className="flex-1 min-w-0">
        <p className="text-[10px] font-mono font-medium uppercase tracking-widest" style={{ color: 'rgba(0,255,136,0.4)' }}>
          {label}
        </p>
        <div className="flex items-baseline gap-2">
          <p className="text-2xl font-bold font-mono tabular-nums" style={{ color: '#00ff88' }}>{value}</p>

          {trend && trend.direction !== 'neutral' && (
            <span
              className="inline-flex items-center text-xs font-mono font-medium gap-0.5"
              style={{ color: trend.direction === 'up' ? '#00ff88' : '#ff4444' }}
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
