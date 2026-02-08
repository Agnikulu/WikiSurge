import { useCallback } from 'react';
import type { TrendingPage } from '../../types';
import { getTrending } from '../../utils/api';
import { useAPI } from '../../hooks/useAPI';
import { usePolling } from '../../hooks/usePolling';
import { useAppStore } from '../../store/appStore';
import { TrendingCard } from './TrendingCard';

export function TrendingList() {
  const updateTrending = useAppStore((s) => s.updateTrending);
  const filters = useAppStore((s) => s.filters);

  const fetcher = useCallback(async () => {
    const language = filters.languages.length === 1 ? filters.languages[0] : undefined;
    return getTrending(20, language);
  }, [filters.languages]);

  const { data: trending, loading, error, refetch } = useAPI<TrendingPage[]>({
    fetcher,
    initialData: [],
  });

  // Poll every 30 seconds
  usePolling({
    fetcher: async () => {
      const language = filters.languages.length === 1 ? filters.languages[0] : undefined;
      const data = await getTrending(20, language);
      updateTrending(data);
    },
    interval: 30000,
  });

  if (loading && (!trending || trending.length === 0)) {
    return (
      <div className="card">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">Trending Pages</h2>
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="animate-pulse">
              <div className="h-4 bg-gray-200 rounded w-3/4 mb-1" />
              <div className="h-3 bg-gray-200 rounded w-1/2" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="card">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">Trending Pages</h2>
        <p className="text-sm text-gray-500 text-center">Failed to load trending pages</p>
        <button onClick={refetch} className="text-primary-600 hover:underline text-sm mt-1 mx-auto block">
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="card">
      <h2 className="text-lg font-semibold text-gray-900 mb-4">Trending Pages</h2>
      <div className="space-y-2 max-h-96 overflow-y-auto">
        {trending && trending.length > 0 ? (
          trending.map((page) => (
            <TrendingCard key={`${page.title}-${page.language}`} page={page} />
          ))
        ) : (
          <p className="text-sm text-gray-400 text-center py-4">No trending pages yet</p>
        )}
      </div>
    </div>
  );
}
