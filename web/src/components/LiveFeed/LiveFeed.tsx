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

  // Sync WebSocket connection state to global store for Header indicators
  const setWsConnected = useAppStore((s) => s.setWsConnected);
  useEffect(() => {
    setWsConnected(connected);
  }, [connected, setWsConnected]);

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
          <h2 style={{ color: '#00ff88', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase' as const }}>LIVE FEED</h2>
          <ConnectionIndicator state={connectionState} reconnectCount={reconnectCount} />
        </div>

        <div className="flex items-center gap-1.5">
          {/* Message rate */}
          {connected && (
            <span className="hidden sm:flex items-center gap-1 text-[11px] font-mono tabular-nums mr-1" style={{ color: 'rgba(0,255,136,0.5)' }}>
              <Activity className="h-3 w-3" />
              {messageRate}/s
            </span>
          )}

          {/* Filter toggle */}
          <button
            onClick={() => setShowFilters((s) => !s)}
            className="p-1.5 rounded-lg transition-colors"
            style={showFilters
              ? { background: 'rgba(0,255,136,0.15)', color: '#00ff88' }
              : { color: 'rgba(0,255,136,0.4)' }
            }
            aria-label="Toggle filters"
            aria-expanded={showFilters}
          >
            <SlidersHorizontal className="h-4 w-4" />
          </button>

          {/* Pause / Resume */}
          <button
            onClick={isPaused ? resume : pause}
            className="p-1.5 rounded-lg transition-colors"
            style={isPaused
              ? { background: 'rgba(255,170,0,0.15)', color: '#ffaa00' }
              : { color: 'rgba(0,255,136,0.4)' }
            }
            aria-label={isPaused ? 'Resume live feed' : 'Pause live feed'}
            title={isPaused ? 'Resume' : 'Pause'}
          >
            {isPaused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
          </button>

          {/* Clear */}
          <button
            onClick={clearData}
            className="p-1.5 rounded-lg transition-colors"
            style={{ color: 'rgba(0,255,136,0.4)' }}
            aria-label="Clear feed"
            title="Clear"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Paused banner */}
      {isPaused && (
        <div className="flex items-center gap-2 px-3 py-1.5 mb-3 rounded-lg text-xs font-medium" style={{ background: 'rgba(255,170,0,0.1)', color: '#ffaa00', border: '1px solid rgba(255,170,0,0.2)', fontFamily: 'monospace' }}>
          <Pause className="h-3 w-3" />
          FEED PAUSED
          <button
            onClick={resume}
            className="ml-auto px-2 py-0.5 rounded transition-colors"
            style={{ background: 'rgba(255,170,0,0.15)', color: '#ffaa00' }}
          >
            RESUME
          </button>
        </div>
      )}

      {/* Filter panel (collapsible) */}
      {showFilters && (
        <div className="mb-3 p-3 rounded-lg animate-slide-down" style={{ background: 'rgba(0,255,136,0.03)', border: '1px solid rgba(0,255,136,0.1)' }}>
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
          <div className="flex flex-col items-center justify-center py-12" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
            <Loader2 className="h-6 w-6 animate-spin mb-2" />
            <span className="text-sm">CONNECTING…</span>
          </div>
        )}

        {/* Error state */}
        {connectionState === 'error' && (
          <div className="flex flex-col items-center justify-center py-12" style={{ fontFamily: 'monospace' }}>
            <WifiOff className="h-6 w-6 mb-2" style={{ color: '#ff4444' }} />
            <span className="text-sm font-medium" style={{ color: '#ff4444' }}>CONNECTION FAILED</span>
            <span className="text-xs mt-1" style={{ color: 'rgba(0,255,136,0.3)' }}>
              Max retries reached ({reconnectCount} attempts)
            </span>
          </div>
        )}

        {/* Disconnected / reconnecting */}
        {connectionState === 'disconnected' && edits.length === 0 && (
          <div className="flex flex-col items-center justify-center py-12" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
            <Loader2 className="h-5 w-5 animate-spin mb-2" />
            <span className="text-sm">RECONNECTING…</span>
            <span className="text-xs mt-0.5" style={{ color: 'rgba(0,255,136,0.3)' }}>Attempt {reconnectCount}</span>
          </div>
        )}

        {/* Empty state */}
        {connected && edits.length === 0 && (
          <div className="flex flex-col items-center justify-center py-12" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
            <Activity className="h-6 w-6 mb-2 opacity-40" />
            <span className="text-sm">WAITING FOR EDITS…</span>
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
          className="absolute bottom-14 right-6 p-2 rounded-full shadow-md transition-all animate-fade-in"
          style={{ background: '#111b2e', border: '1px solid rgba(0,255,136,0.2)', color: '#00ff88' }}
          aria-label="Scroll to top"
        >
          <ArrowUp className="h-4 w-4" />
        </button>
      )}

      {/* Footer stats */}
      {edits.length > 0 && (
        <div className="flex items-center justify-between mt-3 pt-2" style={{ borderTop: '1px solid rgba(0,255,136,0.1)' }}>
          <span className="text-[11px]" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>{edits.length} EDITS IN FEED</span>
          <div className="flex items-center gap-3">
            {connected && messageRate > 0 && (
              <span className="text-[11px] font-mono tabular-nums sm:hidden" style={{ color: 'rgba(0,255,136,0.4)' }}>
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
  const config: Record<ConnectionState, { dotStyle: React.CSSProperties; label: string; labelStyle: React.CSSProperties; icon: React.ReactNode }> = {
    connecting: {
      dotStyle: { background: '#ffaa00' },
      label: 'CONNECTING…',
      labelStyle: { color: '#ffaa00', fontFamily: 'monospace', fontSize: '11px' },
      icon: <Loader2 className="h-3 w-3 animate-spin" />,
    },
    connected: {
      dotStyle: { background: '#00ff88', boxShadow: '0 0 6px rgba(0,255,136,0.5)' },
      label: 'LIVE',
      labelStyle: { color: '#00ff88', fontFamily: 'monospace', fontSize: '11px' },
      icon: <Wifi className="h-3 w-3" />,
    },
    disconnected: {
      dotStyle: { background: '#ff4444' },
      label: `RECONNECTING${reconnectCount > 0 ? ` (${reconnectCount})` : ''}…`,
      labelStyle: { color: '#ff4444', fontFamily: 'monospace', fontSize: '11px' },
      icon: <Loader2 className="h-3 w-3 animate-spin" />,
    },
    error: {
      dotStyle: { background: '#ff4444' },
      label: 'DISCONNECTED',
      labelStyle: { color: '#ff4444', fontFamily: 'monospace', fontSize: '11px' },
      icon: <WifiOff className="h-3 w-3" />,
    },
  };

  const { dotStyle, label, labelStyle } = config[state];

  return (
    <div className="flex items-center gap-1.5" role="status" aria-live="polite">
      <span className="inline-block w-2 h-2 rounded-full" style={dotStyle} aria-hidden="true" />
      <span style={labelStyle}>{label}</span>
    </div>
  );
}
