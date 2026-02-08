import { useCallback, useEffect, useRef, useState } from 'react';
import type { Alert } from '../../types';
import { getAlerts } from '../../utils/api';
import { useWebSocket } from '../../hooks/useWebSocket';
import { usePolling } from '../../hooks/usePolling';
import { buildWebSocketUrl, WS_ENDPOINTS } from '../../utils/websocket';
import { AlertCard } from './AlertCard';
import {
  AlertTriangle,
  Bell,
  BellOff,
  Filter,
  Trash2,
  Wifi,
  WifiOff,
} from 'lucide-react';
import {
  playCriticalAlert,
  playEditWarAlert,
  setAlertSoundsEnabled,
  isAlertSoundsEnabled,
} from '../../utils/alertSounds';

const MAX_ALERTS = 20;
const AUTO_DISMISS_MS = 5 * 60 * 1000; // 5 minutes

const SEVERITY_OPTIONS = ['all', 'critical', 'high', 'medium', 'low'] as const;
const TYPE_OPTIONS = ['all', 'spike', 'edit_war'] as const;

export function AlertsPanel() {
  // ── State ──
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [typeFilter, setTypeFilter] = useState<string>('all');
  const [soundOn, setSoundOn] = useState(isAlertSoundsEnabled());
  const alertsRef = useRef(alerts);
  alertsRef.current = alerts;

  // ── WebSocket (primary data source) ──
  const wsUrl = buildWebSocketUrl(WS_ENDPOINTS.alerts);

  const handleWsMessage = useCallback((data: unknown) => {
    const alert = (data as { data?: Alert })?.data ?? (data as Alert);
    if (!alert || !('type' in alert)) return;

    setAlerts((prev) => {
      // Deduplicate by page_title + type
      const key = `${alert.page_title}-${alert.type}`;
      const exists = prev.some(
        (a) => `${a.page_title}-${a.type}` === key,
      );
      if (exists) return prev;

      const updated = [alert, ...prev].slice(0, MAX_ALERTS);
      return updated;
    });

    // Sound notifications
    if (alert.severity === 'critical') playCriticalAlert();
    else if (alert.type === 'edit_war') playEditWarAlert();
  }, []);

  const {
    connectionState,
    connected,
  } = useWebSocket<Alert>({
    url: wsUrl,
    onMessage: handleWsMessage,
    maxItems: MAX_ALERTS,
  });

  // ── REST fallback polling (when WS disconnected) ──
  const wsDisconnected = connectionState === 'error' || connectionState === 'disconnected';

  usePolling({
    fetcher: async () => {
      try {
        const data = await getAlerts(MAX_ALERTS);
        setAlerts(data);
      } catch {
        // silent – will retry next cycle
      }
    },
    interval: 10_000,
    enabled: wsDisconnected,
  });

  // Also do an initial fetch to populate the list
  const didInitialFetchRef = useRef(false);
  useEffect(() => {
    if (didInitialFetchRef.current) return;
    didInitialFetchRef.current = true;
    getAlerts(MAX_ALERTS)
      .then((data) => setAlerts(data))
      .catch(() => {});
  }, []);

  // ── Auto-dismiss timer ──
  useEffect(() => {
    const timer = setInterval(() => {
      const cutoff = Date.now() - AUTO_DISMISS_MS;
      setAlerts((prev) =>
        prev.filter((a) => {
          const ts = 'timestamp' in a ? a.timestamp : (a as { start_time: string }).start_time;
          return new Date(ts).getTime() > cutoff;
        }),
      );
    }, 30_000);
    return () => clearInterval(timer);
  }, []);

  // ── Handlers ──
  const handleDismiss = useCallback((alert: Alert) => {
    setAlerts((prev) => prev.filter((a) => a !== alert));
  }, []);

  const handleClearAll = useCallback(() => {
    setAlerts([]);
  }, []);

  const toggleSound = useCallback(() => {
    const next = !soundOn;
    setSoundOn(next);
    setAlertSoundsEnabled(next);
  }, [soundOn]);

  // ── Filtered list ──
  const filteredAlerts = alerts.filter((a) => {
    if (severityFilter !== 'all' && a.severity !== severityFilter) return false;
    if (typeFilter !== 'all' && a.type !== typeFilter) return false;
    return true;
  });

  // ── Render ──
  return (
    <div className="card">
      {/* ── Header ── */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mb-4">
        <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
          <AlertTriangle className="h-5 w-5 text-orange-500" />
          🔴 Breaking News Alerts
          {alerts.length > 0 && (
            <span className="badge badge-critical">{alerts.length}</span>
          )}
        </h2>

        <div className="flex items-center gap-2">
          {/* Connection indicator */}
          <span
            className="flex items-center gap-1 text-xs"
            title={connected ? 'Live (WebSocket)' : `Status: ${connectionState}`}
          >
            {connected ? (
              <>
                <Wifi className="h-3 w-3 text-green-500" />
                <span className="text-green-600 hidden sm:inline">Live</span>
              </>
            ) : (
              <>
                <WifiOff className="h-3 w-3 text-red-400" />
                <span className="text-red-500 hidden sm:inline">
                  {wsDisconnected ? 'Polling' : 'Connecting'}
                </span>
              </>
            )}
          </span>

          {/* Sound toggle */}
          <button
            onClick={toggleSound}
            className="p-1 rounded hover:bg-gray-100 transition-colors"
            aria-label={soundOn ? 'Mute alert sounds' : 'Enable alert sounds'}
            title={soundOn ? 'Sound on' : 'Sound off'}
          >
            {soundOn ? (
              <Bell className="h-3.5 w-3.5 text-gray-600" />
            ) : (
              <BellOff className="h-3.5 w-3.5 text-gray-400" />
            )}
          </button>

          {/* Clear all */}
          {alerts.length > 0 && (
            <button
              onClick={handleClearAll}
              className="p-1 rounded hover:bg-gray-100 transition-colors text-gray-400 hover:text-red-500"
              aria-label="Clear all alerts"
              title="Clear all"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
      </div>

      {/* ── Filters ── */}
      <div className="flex flex-wrap gap-3 mb-3">
        {/* Severity filter */}
        <div className="flex items-center gap-1">
          <Filter className="h-3 w-3 text-gray-400" />
          <span className="text-[11px] text-gray-500 mr-1">Severity:</span>
          {SEVERITY_OPTIONS.map((s) => (
            <button
              key={s}
              onClick={() => setSeverityFilter(s)}
              className={`px-2 py-0.5 rounded-full text-[11px] font-medium transition-all ${
                severityFilter === s
                  ? 'bg-blue-100 text-blue-700 ring-1 ring-blue-200'
                  : 'bg-gray-100 text-gray-500 hover:bg-gray-200'
              }`}
            >
              {s === 'all' ? 'All' : s.charAt(0).toUpperCase() + s.slice(1)}
            </button>
          ))}
        </div>

        {/* Type filter */}
        <div className="flex items-center gap-1">
          <span className="text-[11px] text-gray-500 mr-1">Type:</span>
          {TYPE_OPTIONS.map((t) => (
            <button
              key={t}
              onClick={() => setTypeFilter(t)}
              className={`px-2 py-0.5 rounded-full text-[11px] font-medium transition-all ${
                typeFilter === t
                  ? 'bg-blue-100 text-blue-700 ring-1 ring-blue-200'
                  : 'bg-gray-100 text-gray-500 hover:bg-gray-200'
              }`}
            >
              {t === 'all' ? 'All' : t === 'spike' ? 'Spike' : 'Edit War'}
            </button>
          ))}
        </div>
      </div>

      {/* ── Alert list ── */}
      <div className="space-y-2 max-h-[500px] overflow-y-auto scrollbar-thin">
        {filteredAlerts.length > 0 ? (
          filteredAlerts.map((alert, index) => (
            <AlertCard
              key={`${alert.page_title}-${alert.type}-${index}`}
              alert={alert}
              onDismiss={handleDismiss}
            />
          ))
        ) : alerts.length > 0 ? (
          <div className="text-center py-8 text-sm text-gray-400">
            <Filter className="h-6 w-6 mx-auto mb-2 text-gray-300" />
            No alerts match the current filters
          </div>
        ) : (
          <div className="text-center py-8 text-sm text-gray-400">
            <AlertTriangle className="h-8 w-8 mx-auto mb-2 text-gray-300" />
            No alerts yet. Waiting for spikes…
          </div>
        )}
      </div>
    </div>
  );
}
