import { useState, useRef, useCallback, useEffect, useMemo } from 'react';
import type { Edit } from '../../types';
import { useWebSocket } from '../../hooks/useWebSocket';
import type { ConnectionState } from '../../hooks/useWebSocket';
import { buildWebSocketUrl, WS_ENDPOINTS } from '../../utils/websocket';
import { useAppStore } from '../../store/appStore';
import { EditItem } from './EditItem';
import { FilterControls } from './FilterControls';
import { EditDetailsModal } from './EditDetailsModal';
import {
  Wifi,
  WifiOff,
  Pause,
  Play,
  Trash2,
  Loader2,
  Activity,
  SlidersHorizontal,
  ArrowUp,
} from 'lucide-react';

export function LiveFeed() {
  const filters = useAppStore((s) => s.filters);

  // Build WS filter params to match backend query params
  const wsFilter = useMemo(() => {
    const params: Record<string, string> = {};
    if (filters.languages.length > 0) {
      params.languages = filters.languages.join(',');
    }
    if (filters.excludeBots) {
      params.exclude_bots = 'true';
    }
    if (filters.minByteChange > 0) {
      params.min_byte_change = String(filters.minByteChange);
    }
    return params;
  }, [filters.languages, filters.excludeBots, filters.minByteChange]);

  // We rebuild the URL string so the hook reconnects when filters change
  const wsUrl = useMemo(() => {
    return buildWebSocketUrl(WS_ENDPOINTS.feed, wsFilter);
  }, [wsFilter]);

  const {
    data: edits,
    connectionState,
    connected,
    reconnectCount,
    messageRate,
    clearData,
    pause,
    resume,
    isPaused,
  } = useWebSocket<Edit>({ url: wsUrl });

  const [showFilters, setShowFilters] = useState(false);
  const [selectedEdit, setSelectedEdit] = useState<Edit | null>(null);
  const [userScrolled, setUserScrolled] = useState(false);

  const feedRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to top on new edits (only if user hasn't scrolled down)
  useEffect(() => {
    if (!userScrolled && !isPaused && feedRef.current) {
      feedRef.current.scrollTo({ top: 0, behavior: 'smooth' });
    }
  }, [edits, userScrolled, isPaused]);

  // Detect user scroll
  const handleScroll = useCallback(() => {
    if (!feedRef.current) return;
    const { scrollTop } = feedRef.current;
    setUserScrolled(scrollTop > 40);
  }, []);

  const scrollToTop = useCallback(() => {
    feedRef.current?.scrollTo({ top: 0, behavior: 'smooth' });
    setUserScrolled(false);
  }, []);

  const handleEditClick = useCallback((edit: Edit) => {
    setSelectedEdit(edit);
  }, []);

  const handleCloseModal = useCallback(() => {
    setSelectedEdit(null);
  }, []);

  return (
    <div className="card flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <h2 className="text-lg font-semibold text-gray-900">Live Edit Feed</h2>
          <ConnectionIndicator state={connectionState} reconnectCount={reconnectCount} />
        </div>

        <div className="flex items-center gap-1.5">
          {/* Message rate */}
          {connected && (
            <span className="hidden sm:flex items-center gap-1 text-[11px] text-gray-400 font-mono tabular-nums mr-1">
              <Activity className="h-3 w-3" />
              {messageRate}/s
            </span>
          )}

          {/* Filter toggle */}
          <button
            onClick={() => setShowFilters((s) => !s)}
            className={`p-1.5 rounded-lg transition-colors ${
              showFilters ? 'bg-blue-50 text-blue-600' : 'text-gray-400 hover:text-gray-600 hover:bg-gray-100'
            }`}
            aria-label="Toggle filters"
            aria-expanded={showFilters}
          >
            <SlidersHorizontal className="h-4 w-4" />
          </button>

          {/* Pause / Resume */}
          <button
            onClick={isPaused ? resume : pause}
            className={`p-1.5 rounded-lg transition-colors ${
              isPaused
                ? 'bg-amber-50 text-amber-600'
                : 'text-gray-400 hover:text-gray-600 hover:bg-gray-100'
            }`}
            aria-label={isPaused ? 'Resume live feed' : 'Pause live feed'}
            title={isPaused ? 'Resume' : 'Pause'}
          >
            {isPaused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
          </button>

          {/* Clear */}
          <button
            onClick={clearData}
            className="p-1.5 rounded-lg text-gray-400 hover:text-gray-600 hover:bg-gray-100 transition-colors"
            aria-label="Clear feed"
            title="Clear"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Paused banner */}
      {isPaused && (
        <div className="flex items-center gap-2 px-3 py-1.5 mb-3 rounded-lg bg-amber-50 text-amber-700 text-xs font-medium">
          <Pause className="h-3 w-3" />
          Feed paused — new edits are not being displayed
          <button
            onClick={resume}
            className="ml-auto px-2 py-0.5 rounded bg-amber-100 hover:bg-amber-200 transition-colors"
          >
            Resume
          </button>
        </div>
      )}

      {/* Filter panel (collapsible) */}
      {showFilters && (
        <div className="mb-3 p-3 bg-gray-50 rounded-lg border border-gray-100 animate-slide-down">
          <FilterControls />
        </div>
      )}

      {/* Feed */}
      <div
        ref={feedRef}
        onScroll={handleScroll}
        className="relative space-y-0.5 max-h-[400px] overflow-y-auto scroll-smooth overscroll-contain
          scrollbar-thin scrollbar-thumb-gray-200 scrollbar-track-transparent"
        role="feed"
        aria-label="Live edit feed"
        aria-busy={connectionState === 'connecting'}
      >
        {/* Loading state */}
        {connectionState === 'connecting' && edits.length === 0 && (
          <div className="flex flex-col items-center justify-center py-12 text-gray-400">
            <Loader2 className="h-6 w-6 animate-spin mb-2" />
            <span className="text-sm">Connecting to live feed…</span>
          </div>
        )}

        {/* Error state */}
        {connectionState === 'error' && (
          <div className="flex flex-col items-center justify-center py-12 text-red-400">
            <WifiOff className="h-6 w-6 mb-2" />
            <span className="text-sm font-medium">Connection failed</span>
            <span className="text-xs text-gray-400 mt-1">
              Max retries reached ({reconnectCount} attempts)
            </span>
          </div>
        )}

        {/* Disconnected / reconnecting */}
        {connectionState === 'disconnected' && edits.length === 0 && (
          <div className="flex flex-col items-center justify-center py-12 text-gray-400">
            <Loader2 className="h-5 w-5 animate-spin mb-2" />
            <span className="text-sm">Reconnecting…</span>
            <span className="text-xs text-gray-300 mt-0.5">Attempt {reconnectCount}</span>
          </div>
        )}

        {/* Empty state */}
        {connected && edits.length === 0 && (
          <div className="flex flex-col items-center justify-center py-12 text-gray-400">
            <Activity className="h-6 w-6 mb-2 opacity-40" />
            <span className="text-sm">Waiting for edits…</span>
          </div>
        )}

        {/* Edit list */}
        {edits.map((edit, index) => (
          <div
            key={`${edit.id}-${edit.revision?.new ?? index}`}
            className={index === 0 && !isPaused ? 'animate-feed-in' : ''}
          >
            <EditItem edit={edit} onClick={handleEditClick} />
          </div>
        ))}
      </div>

      {/* Scroll-to-top button */}
      {userScrolled && edits.length > 0 && (
        <button
          onClick={scrollToTop}
          className="absolute bottom-14 right-6 p-2 rounded-full bg-white shadow-md border border-gray-200 text-gray-500 hover:text-gray-700 hover:shadow-lg transition-all animate-fade-in"
          aria-label="Scroll to top"
        >
          <ArrowUp className="h-4 w-4" />
        </button>
      )}

      {/* Footer stats */}
      {edits.length > 0 && (
        <div className="flex items-center justify-between mt-3 pt-2 border-t border-gray-100">
          <span className="text-[11px] text-gray-400">{edits.length} edits in feed</span>
          <div className="flex items-center gap-3">
            {connected && messageRate > 0 && (
              <span className="text-[11px] text-gray-400 font-mono tabular-nums sm:hidden">
                {messageRate}/s
              </span>
            )}
          </div>
        </div>
      )}

      {/* Edit Details Modal */}
      {selectedEdit && <EditDetailsModal edit={selectedEdit} onClose={handleCloseModal} />}
    </div>
  );
}

/** Connection status indicator */
function ConnectionIndicator({
  state,
  reconnectCount,
}: {
  state: ConnectionState;
  reconnectCount: number;
}) {
  const config: Record<ConnectionState, { color: string; label: string; icon: React.ReactNode }> = {
    connecting: {
      color: 'bg-yellow-400 animate-pulse',
      label: 'Connecting…',
      icon: <Loader2 className="h-3 w-3 animate-spin" />,
    },
    connected: {
      color: 'bg-green-500',
      label: 'Live',
      icon: <Wifi className="h-3 w-3" />,
    },
    disconnected: {
      color: 'bg-red-400 animate-pulse',
      label: `Reconnecting${reconnectCount > 0 ? ` (${reconnectCount})` : ''}…`,
      icon: <Loader2 className="h-3 w-3 animate-spin" />,
    },
    error: {
      color: 'bg-red-500',
      label: 'Disconnected',
      icon: <WifiOff className="h-3 w-3" />,
    },
  };

  const { color, label } = config[state];

  return (
    <div className="flex items-center gap-1.5" role="status" aria-live="polite">
      <span className={`inline-block w-2 h-2 rounded-full ${color}`} aria-hidden="true" />
      <span className="text-xs text-gray-500">{label}</span>
    </div>
  );
}
