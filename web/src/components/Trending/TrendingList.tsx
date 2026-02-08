import { useCallback, useRef, useMemo, useState } from 'react';
import type { TrendingPage } from '../../types';
import { getTrending } from '../../utils/api';
import { usePollingData } from '../../hooks/usePolling';
import { useAppStore } from '../../store/appStore';
import { TrendingCard } from './TrendingCard';
import { RefreshCw, TrendingUp } from 'lucide-react';
import { formatRelativeTime } from '../../utils/formatting';

const COMMON_LANGUAGES = [
  { code: '', label: 'All' },
  { code: 'en', label: 'EN' },
  { code: 'es', label: 'ES' },
  { code: 'fr', label: 'FR' },
  { code: 'de', label: 'DE' },
  { code: 'ja', label: 'JA' },
  { code: 'zh', label: 'ZH' },
];

export function TrendingList() {
  const updateTrending = useAppStore((s) => s.updateTrending);
  const filters = useAppStore((s) => s.filters);

  // Local language filter (independent override)
  const [langFilter, setLangFilter] = useState<string>('');
  const activeLang = langFilter || (filters.languages.length === 1 ? filters.languages[0] : undefined) || '';

  // Keep previous titles to detect new entries
  const prevTitlesRef = useRef<Set<string>>(new Set());

  const fetchFunction = useCallback(async () => {
    const language = activeLang || undefined;
    const data = await getTrending(20, language);
    updateTrending(data);
    return data;
  }, [activeLang, updateTrending]);

  const {
    data: trending,
    loading,
    error,
    refresh,
    lastUpdate,
  } = usePollingData<TrendingPage[]>({
    fetchFunction,
    interval: 10_000,
  });

  // Determine which titles are new since previous fetch
  const newTitles = useMemo(() => {
    if (!trending) return new Set<string>();
    const currentTitles = new Set(trending.map((p) => p.title));
    const additions = new Set<string>();
    currentTitles.forEach((t) => {
      if (!prevTitlesRef.current.has(t)) additions.add(t);
    });
    prevTitlesRef.current = currentTitles;
    return additions;
  }, [trending]);

  const [refreshing, setRefreshing] = useState(false);
  const handleManualRefresh = async () => {
    setRefreshing(true);
    await refresh();
    setRefreshing(false);
  };

  // ── Loading skeleton ──
  if (loading && (!trending || trending.length === 0)) {
    return (
      <div className="card">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
            <TrendingUp className="h-5 w-5 text-green-500" />
            📈 Trending Pages
          </h2>
        </div>
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="animate-pulse flex items-center gap-3 p-3">
              <div className="h-8 w-8 bg-gray-200 rounded-lg" />
              <div className="flex-1">
                <div className="h-4 bg-gray-200 rounded w-3/4 mb-1" />
                <div className="h-3 bg-gray-200 rounded w-1/2" />
              </div>
              <div className="h-4 w-12 bg-gray-200 rounded" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  // ── Error state ──
  if (error) {
    return (
      <div className="card">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
            <TrendingUp className="h-5 w-5 text-green-500" />
            📈 Trending Pages
          </h2>
        </div>
        <div className="text-center py-6">
          <p className="text-sm text-red-600 mb-2">Failed to load trending pages</p>
          <button
            onClick={handleManualRefresh}
            className="text-sm text-blue-600 hover:underline"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="card">
      {/* ── Header ── */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mb-4">
        <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
          <TrendingUp className="h-5 w-5 text-green-500" />
          📈 Trending Pages
        </h2>

        <div className="flex items-center gap-2 text-xs text-gray-400">
          {lastUpdate && (
            <span className="hidden sm:inline">
              Updated {formatRelativeTime(lastUpdate.toISOString())}
            </span>
          )}
          <button
            onClick={handleManualRefresh}
            disabled={refreshing}
            className="p-1 rounded hover:bg-gray-100 transition-colors disabled:opacity-50"
            aria-label="Refresh trending"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {/* ── Language filter ── */}
      <div className="flex flex-wrap gap-1 mb-3">
        {COMMON_LANGUAGES.map(({ code, label }) => (
          <button
            key={code || 'all'}
            onClick={() => setLangFilter(code)}
            className={`px-2 py-0.5 rounded-full text-[11px] font-medium transition-all duration-150 ${
              activeLang === code || (code === '' && !activeLang)
                ? 'bg-blue-100 text-blue-700 ring-1 ring-blue-200'
                : 'bg-gray-100 text-gray-500 hover:bg-gray-200'
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      {/* ── List ── */}
      <div className="space-y-1 max-h-[500px] overflow-y-auto scrollbar-thin">
        {trending && trending.length > 0 ? (
          trending.map((page, idx) => (
            <TrendingCard
              key={`${page.title}-${page.language}`}
              page={page}
              rank={idx + 1}
              isNew={newTitles.has(page.title)}
            />
          ))
        ) : (
          <div className="text-center py-8 text-sm text-gray-400">
            <TrendingUp className="h-8 w-8 mx-auto mb-2 text-gray-300" />
            No trending pages yet
          </div>
        )}
      </div>

      {/* ── Footer summary ── */}
      {trending && trending.length > 0 && (
        <div className="mt-3 pt-3 border-t border-gray-100 flex items-center justify-between text-xs text-gray-400">
          <span>
            Showing top {trending.length} page{trending.length !== 1 ? 's' : ''}
          </span>
          {activeLang && (
            <span className="uppercase font-medium text-blue-500">{activeLang}</span>
          )}
        </div>
      )}
    </div>
  );
}
