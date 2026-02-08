import { useCallback, useEffect, useRef, useState } from 'react';
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
const POLL_INTERVAL = 15_000;

/** Sort: by severity weight (desc), then by start_time (desc) */
const SEVERITY_WEIGHT: Record<string, number> = {
  critical: 4,
  high: 3,
  medium: 2,
  low: 1,
};

function sortWars(a: EditWar, b: EditWar): number {
  const wa = SEVERITY_WEIGHT[a.severity.toLowerCase()] ?? 0;
  const wb = SEVERITY_WEIGHT[b.severity.toLowerCase()] ?? 0;
  if (wb !== wa) return wb - wa;
  return new Date(b.start_time).getTime() - new Date(a.start_time).getTime();
}

export function EditWarsList() {
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
          className="flex items-center gap-2 text-lg font-semibold text-gray-900 hover:text-gray-700 transition-colors"
        >
          <Swords className="h-5 w-5 text-red-500" />
          <span>⚔️ Edit Wars in Progress</span>
          {activeCount > 0 && (
            <span className="badge badge-critical">{activeCount}</span>
          )}
          {collapsed ? (
            <ChevronDown className="h-4 w-4 text-gray-400" />
          ) : (
            <ChevronUp className="h-4 w-4 text-gray-400" />
          )}
        </button>

        {!collapsed && (
          <div className="flex items-center gap-2">
            {/* Filter toggle */}
            <div className="inline-flex rounded-md shadow-sm" role="group">
              <button
                onClick={() => setFilter('active')}
                className={`px-3 py-1 text-xs font-medium rounded-l-md border transition-colors ${
                  filter === 'active'
                    ? 'bg-red-50 text-red-700 border-red-200'
                    : 'bg-white text-gray-600 border-gray-200 hover:bg-gray-50'
                }`}
              >
                Active
              </button>
              <button
                onClick={() => setFilter('all')}
                className={`px-3 py-1 text-xs font-medium rounded-r-md border-t border-r border-b transition-colors ${
                  filter === 'all'
                    ? 'bg-red-50 text-red-700 border-red-200'
                    : 'bg-white text-gray-600 border-gray-200 hover:bg-gray-50'
                }`}
              >
                All
              </button>
            </div>

            <Filter className="h-3.5 w-3.5 text-gray-400" />
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
            <div className="space-y-3 max-h-[600px] overflow-y-auto scrollbar-thin pr-1">
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
}

/* ── Sub-components ─────────────────────────────────── */

function LoadingSkeleton() {
  return (
    <div className="animate-pulse space-y-3">
      {Array.from({ length: 3 }).map((_, i) => (
        <div key={i} className="p-4 rounded-lg border border-gray-200">
          <div className="h-4 bg-gray-200 rounded w-2/3 mb-2" />
          <div className="h-3 bg-gray-200 rounded w-1/2 mb-3" />
          <div className="grid grid-cols-4 gap-3">
            {Array.from({ length: 4 }).map((_, j) => (
              <div key={j} className="h-3 bg-gray-200 rounded" />
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
      <p className="text-sm text-gray-500">Failed to load edit wars</p>
      <button
        onClick={onRetry}
        className="text-primary-600 hover:underline text-sm mt-2"
      >
        Retry
      </button>
    </div>
  );
}

function EmptyState({ filter }: { filter: 'active' | 'all' }) {
  return (
    <div className="text-center py-8">
      <Swords className="h-10 w-10 text-gray-300 mx-auto mb-2" />
      <p className="text-sm text-gray-400">
        {filter === 'active'
          ? 'No active edit wars detected'
          : 'No edit wars found'}
      </p>
      <p className="text-xs text-gray-300 mt-1">
        Edit wars will appear here when detected
      </p>
    </div>
  );
}
