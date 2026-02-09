import { useCallback, useEffect, useRef, useState, memo } from 'react';
import type { EditWar } from '../../types';
import { getEditWars } from '../../utils/api';
import { useAPI } from '../../hooks/useAPI';
import { usePolling } from '../../hooks/usePolling';
import { useWebSocket } from '../../hooks/useWebSocket';
import { buildWebSocketUrl, WS_ENDPOINTS } from '../../utils/websocket';
import { EditWarCard } from './EditWarCard';
import { playEditWarAlert } from '../../utils/alertSounds';
import { requestNotificationPermission, showEditWarNotification } from '../../utils/notifications';
import {
  Swords,
  Filter,
  ChevronDown,
  ChevronUp,
} from 'lucide-react';

const RESOLVED_LINGER_MS = 60_000; // auto-remove resolved wars after 1 min
const POLL_INTERVAL = 20_000; // Reduced polling frequency

/** Sort: by severity weight (desc), then by start_time (desc) */
const SEVERITY_WEIGHT: Record<string, number> = {
  critical: 4,
  high: 3,
  medium: 2,
  low: 1,
};

function sortWars(a: EditWar, b: EditWar): number {
  const wa = SEVERITY_WEIGHT[a.severity?.toLowerCase()] ?? 0;
  const wb = SEVERITY_WEIGHT[b.severity?.toLowerCase()] ?? 0;
  if (wb !== wa) return wb - wa;
  const ta = a.start_time ? new Date(a.start_time).getTime() : 0;
  const tb = b.start_time ? new Date(b.start_time).getTime() : 0;
  return tb - ta;
}

export const EditWarsList = memo(function EditWarsList() {
  const [filter, setFilter] = useState<'active' | 'all'>('active');
  const [collapsed, setCollapsed] = useState(false);
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());
  const [newKeys, setNewKeys] = useState<Set<string>>(new Set());
  const prevKeysRef = useRef<Set<string>>(new Set());
  const resolvedTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  // ── Data fetching ──────────────────────────────────
  const fetcher = useCallback(
    () => getEditWars(filter === 'active'),
    [filter],
  );

  const { data: editWars, loading, error, refetch } = useAPI<EditWar[]>({
    fetcher,
    initialData: [],
  });

  // Re-fetch when filter changes
  useEffect(() => {
    refetch();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter]);

  // Poll every 15 seconds
  usePolling({ fetcher: refetch, interval: POLL_INTERVAL });

  // ── WebSocket real-time updates ────────────────────
  const wsUrl = buildWebSocketUrl(WS_ENDPOINTS.alerts);
  useWebSocket<{ type: string; data: EditWar }>({
    url: wsUrl,
    onMessage: (msg) => {
      const parsed = msg as { type?: string; data?: EditWar };
      if (parsed.type === 'edit_war' && parsed.data) {
        // Trigger a refetch to stay in sync
        refetch();
      }
    },
  });

  // ── Request notification permission on mount ───────
  useEffect(() => {
    requestNotificationPermission();
  }, []);

  // ── Detect new wars, highlight, and notify ─────────
  useEffect(() => {
    if (!editWars || editWars.length === 0) return;

    const currentKeys = new Set(editWars.map((w) => w.page_title));
    const prev = prevKeysRef.current;
    const added = new Set<string>();

    currentKeys.forEach((key) => {
      if (!prev.has(key)) added.add(key);
    });

    if (added.size > 0) {
      setNewKeys(added);
      // Clear highlight after 5 seconds
      setTimeout(() => setNewKeys(new Set()), 5000);

      // Play sound for new wars
      playEditWarAlert();

      // Browser notification for each new war
      editWars.forEach((w) => {
        if (added.has(w.page_title)) {
          showEditWarNotification(w);
        }
      });
    }

    prevKeysRef.current = currentKeys;
  }, [editWars]);

  // ── Auto-remove resolved wars after 1 min ──────────
  useEffect(() => {
    if (!editWars) return;
    editWars.forEach((war) => {
      const key = war.page_title;
      if (!war.active && !resolvedTimers.current.has(key)) {
        const timer = setTimeout(() => {
          setDismissed((prev) => new Set(prev).add(key));
          resolvedTimers.current.delete(key);
        }, RESOLVED_LINGER_MS);
        resolvedTimers.current.set(key, timer);
      }
    });

    return () => {
      resolvedTimers.current.forEach((t) => clearTimeout(t));
    };
  }, [editWars]);

  // ── Handlers ───────────────────────────────────────
  const handleDismiss = useCallback((war: EditWar) => {
    setDismissed((prev) => new Set(prev).add(war.page_title));
  }, []);

  // ── Filtered & sorted list ─────────────────────────
  const visibleWars = (editWars ?? [])
    .filter((w) => !dismissed.has(w.page_title))
    .sort(sortWars);

  const activeCount = visibleWars.filter((w) => w.active).length;

  // ── Render ─────────────────────────────────────────
  return (
    <div className="card">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="flex items-center gap-2 transition-colors"
          style={{ color: '#ff4444', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase' as const }}
        >
          <Swords className="h-5 w-5" style={{ color: '#ff4444' }} />
          <span>EDIT WARS IN PROGRESS</span>
          {activeCount > 0 && (
            <span className="badge badge-critical">{activeCount}</span>
          )}
          {collapsed ? (
            <ChevronDown className="h-4 w-4" style={{ color: 'rgba(0,255,136,0.4)' }} />
          ) : (
            <ChevronUp className="h-4 w-4" style={{ color: 'rgba(0,255,136,0.4)' }} />
          )}
        </button>

        {!collapsed && (
          <div className="flex items-center gap-2">
            {/* Filter toggle */}
            <div className="inline-flex rounded-md" role="group">
              <button
                onClick={() => setFilter('active')}
                className="px-3 py-1 text-xs font-medium rounded-l-md transition-colors"
                style={filter === 'active'
                  ? { background: 'rgba(255,68,68,0.15)', color: '#ff4444', border: '1px solid rgba(255,68,68,0.3)', fontFamily: 'monospace' }
                  : { background: 'rgba(0,255,136,0.05)', color: 'rgba(0,255,136,0.4)', border: '1px solid rgba(0,255,136,0.1)', fontFamily: 'monospace' }
                }
              >
                ACTIVE
              </button>
              <button
                onClick={() => setFilter('all')}
                className="px-3 py-1 text-xs font-medium rounded-r-md transition-colors"
                style={filter === 'all'
                  ? { background: 'rgba(255,68,68,0.15)', color: '#ff4444', border: '1px solid rgba(255,68,68,0.3)', fontFamily: 'monospace' }
                  : { background: 'rgba(0,255,136,0.05)', color: 'rgba(0,255,136,0.4)', border: '1px solid rgba(0,255,136,0.1)', fontFamily: 'monospace' }
                }
              >
                ALL
              </button>
            </div>

            <Filter className="h-3.5 w-3.5" style={{ color: 'rgba(0,255,136,0.3)' }} />
          </div>
        )}
      </div>

      {/* Body */}
      {!collapsed && (
        <>
          {loading && visibleWars.length === 0 ? (
            <LoadingSkeleton />
          ) : error ? (
            <ErrorState onRetry={refetch} />
          ) : visibleWars.length === 0 ? (
            <EmptyState filter={filter} />
          ) : (
            <div className="space-y-3 max-h-[600px] overflow-y-auto scrollbar-thin pr-1" role="list" aria-label="Edit wars">
              {visibleWars.map((war) => (
                <EditWarCard
                  key={war.page_title}
                  war={war}
                  onDismiss={!war.active ? handleDismiss : undefined}
                  isNew={newKeys.has(war.page_title)}
                />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
});

/* ── Sub-components ─────────────────────────────────── */

function LoadingSkeleton() {
  return (
    <div className="animate-pulse space-y-3">
      {Array.from({ length: 3 }).map((_, i) => (
        <div key={i} className="p-4 rounded-lg" style={{ border: '1px solid rgba(0,255,136,0.08)' }}>
          <div className="h-4 rounded w-2/3 mb-2" style={{ background: 'rgba(0,255,136,0.06)' }} />
          <div className="h-3 rounded w-1/2 mb-3" style={{ background: 'rgba(0,255,136,0.06)' }} />
          <div className="grid grid-cols-4 gap-3">
            {Array.from({ length: 4 }).map((_, j) => (
              <div key={j} className="h-3 rounded" style={{ background: 'rgba(0,255,136,0.06)' }} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function ErrorState({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="text-center py-8">
      <p className="text-sm" style={{ color: '#ff4444', fontFamily: 'monospace' }}>FAILED TO LOAD EDIT WARS</p>
      <button
        onClick={onRetry}
        className="text-sm mt-2" style={{ color: '#00ff88', fontFamily: 'monospace' }}
      >
        RETRY
      </button>
    </div>
  );
}

function EmptyState({ filter }: { filter: 'active' | 'all' }) {
  return (
    <div className="text-center py-8">
      <Swords className="h-10 w-10 mx-auto mb-2" style={{ color: 'rgba(0,255,136,0.15)' }} />
      <p className="text-sm" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
        {filter === 'active'
          ? 'NO ACTIVE EDIT WARS DETECTED'
          : 'NO EDIT WARS FOUND'}
      </p>
      <p className="text-xs mt-1" style={{ color: 'rgba(0,255,136,0.25)', fontFamily: 'monospace' }}>
        MONITORING FOR CONFLICTS…
      </p>
    </div>
  );
}
