import { memo } from 'react';

interface SkeletonProps {
  className?: string;
  /** Number of repeating lines */
  lines?: number;
  /** Height variant */
  variant?: 'text' | 'title' | 'card' | 'chart' | 'stat' | 'circle';
}

/** Basic pulsing skeleton block */
export const Skeleton = memo(function Skeleton({ className = '', variant = 'text', lines = 1 }: SkeletonProps) {
  const base = 'animate-pulse rounded bg-gray-200 dark:bg-gray-700';

  const variantClasses: Record<string, string> = {
    text: 'h-4 w-full',
    title: 'h-6 w-3/4',
    card: 'h-32 w-full rounded-lg',
    chart: 'h-64 w-full rounded-lg',
    stat: 'h-20 w-full rounded-lg',
    circle: 'h-10 w-10 rounded-full',
  };

  if (lines > 1) {
    return (
      <div className={`space-y-3 ${className}`}>
        {Array.from({ length: lines }).map((_, i) => (
          <div
            key={i}
            className={`${base} ${variantClasses[variant]}`}
            style={variant === 'text' ? { width: `${Math.max(40, 100 - i * 15)}%` } : undefined}
          />
        ))}
      </div>
    );
  }

  return <div className={`${base} ${variantClasses[variant]} ${className}`} />;
});

/** Skeleton layout matching StatsOverview */
export const StatsOverviewSkeleton = memo(function StatsOverviewSkeleton() {
  return (
    <div className="card dark:bg-gray-800 dark:border-gray-700">
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-4">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="space-y-2 p-3">
            <Skeleton variant="text" className="w-1/2" />
            <Skeleton variant="title" className="w-3/4" />
          </div>
        ))}
      </div>
    </div>
  );
});

/** Skeleton layout matching TrendingList */
export const TrendingListSkeleton = memo(function TrendingListSkeleton() {
  return (
    <div className="card dark:bg-gray-800 dark:border-gray-700 space-y-3">
      <Skeleton variant="title" className="w-32" />
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex items-center space-x-3 py-2">
          <Skeleton variant="circle" />
          <div className="flex-1 space-y-2">
            <Skeleton variant="text" className="w-2/3" />
            <Skeleton variant="text" className="w-1/4" />
          </div>
        </div>
      ))}
    </div>
  );
});

/** Skeleton layout matching a chart card */
export const ChartSkeleton = memo(function ChartSkeleton() {
  return (
    <div className="card dark:bg-gray-800 dark:border-gray-700 space-y-3">
      <Skeleton variant="title" className="w-40" />
      <Skeleton variant="chart" />
    </div>
  );
});

/** Skeleton layout matching SearchInterface */
export const SearchSkeleton = memo(function SearchSkeleton() {
  return (
    <div className="card dark:bg-gray-800 dark:border-gray-700 space-y-3">
      <Skeleton variant="title" className="w-28" />
      <Skeleton variant="text" className="h-10 w-full rounded-md" />
      <Skeleton lines={4} />
    </div>
  );
});
