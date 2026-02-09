import { useCallback, useRef, useMemo, useState, memo } from 'react';
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

export const TrendingList = memo(function TrendingList() {
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
          <h2 className="flex items-center gap-2" style={{ color: '#00ff88', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase' as const }}>
            <TrendingUp className="h-5 w-5" style={{ color: '#00ff88' }} />
            TRENDING PAGES
          </h2>
        </div>
        <div className="space-y-3">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="animate-pulse flex items-center gap-3 p-3">
              <div className="h-8 w-8 rounded-lg" style={{ background: 'rgba(0,255,136,0.06)' }} />
              <div className="flex-1">
                <div className="h-4 rounded w-3/4 mb-1" style={{ background: 'rgba(0,255,136,0.06)' }} />
                <div className="h-3 rounded w-1/2" style={{ background: 'rgba(0,255,136,0.06)' }} />
              </div>
              <div className="h-4 w-12 rounded" style={{ background: 'rgba(0,255,136,0.06)' }} />
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
          <h2 className="flex items-center gap-2" style={{ color: '#00ff88', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase' as const }}>
            <TrendingUp className="h-5 w-5" style={{ color: '#00ff88' }} />
            TRENDING PAGES
          </h2>
        </div>
        <div className="text-center py-6">
          <p className="text-sm mb-2" style={{ color: '#ff4444', fontFamily: 'monospace' }}>FAILED TO LOAD TRENDING</p>
          <button
            onClick={handleManualRefresh}
            className="text-sm" style={{ color: '#00ff88', fontFamily: 'monospace' }}
          >
            RETRY
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="card">
      {/* ── Header ── */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mb-4">
        <h2 className="flex items-center gap-2" style={{ color: '#00ff88', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase' as const }}>
          <TrendingUp className="h-5 w-5" style={{ color: '#00ff88' }} />
          TRENDING PAGES
        </h2>

        <div className="flex items-center gap-2 text-xs" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
          {lastUpdate && (
            <span className="hidden sm:inline">
              Updated {formatRelativeTime(lastUpdate.toISOString())}
            </span>
          )}
          <button
            onClick={handleManualRefresh}
            disabled={refreshing}
            className="p-1 rounded transition-colors disabled:opacity-50"
            style={{ color: '#00ff88' }}
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
            className="px-2 py-0.5 rounded-full text-[11px] font-medium transition-all duration-150"
            style={activeLang === code || (code === '' && !activeLang)
              ? { background: 'rgba(0,255,136,0.15)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.3)', fontFamily: 'monospace' }
              : { background: 'rgba(0,255,136,0.05)', color: 'rgba(0,255,136,0.4)', border: '1px solid transparent', fontFamily: 'monospace' }
            }
          >
            {label}
          </button>
        ))}
      </div>

      {/* ── List ── */}
      <div className="space-y-1 max-h-[500px] overflow-y-auto scrollbar-thin" role="list" aria-label="Trending pages">
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
          <div className="text-center py-8 text-sm" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
            <TrendingUp className="h-8 w-8 mx-auto mb-2" style={{ color: 'rgba(0,255,136,0.2)' }} />
            NO TRENDING PAGES YET
          </div>
        )}
      </div>

      {/* ── Footer summary ── */}
      {trending && trending.length > 0 && (
        <div className="mt-3 pt-3 flex items-center justify-between text-xs" style={{ borderTop: '1px solid rgba(0,255,136,0.1)', color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
          <span>
            SHOWING TOP {trending.length} PAGE{trending.length !== 1 ? 'S' : ''}
          </span>
          {activeLang && (
            <span className="uppercase font-medium" style={{ color: '#00ddff' }}>{activeLang}</span>
          )}
        </div>
      )}
    </div>
  );
});
