import { useCallback, useEffect, useRef, useState, useMemo, memo } from 'react';
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

export const AlertsPanel = memo(function AlertsPanel() {
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
    try {
      const alert = (data as { data?: Alert })?.data ?? (data as Alert);
      
      // Defensive: validate alert has required fields
      if (!alert || typeof alert !== 'object') {
        console.warn('[AlertsPanel] Invalid alert data (not an object):', data);
        return;
      }
      
      if (!('type' in alert) || !('page_title' in alert) || !('severity' in alert)) {
        console.warn('[AlertsPanel] Alert missing required fields:', alert);
        return;
      }

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
    } catch (error) {
      console.error('[AlertsPanel] Error handling WebSocket message:', error, data);
    }
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

  // Initial fetch only if WebSocket fails to connect within 3 seconds
  const didInitialFetchRef = useRef(false);
  useEffect(() => {
    if (didInitialFetchRef.current) return;
    if (connected) {
      didInitialFetchRef.current = true;
      return;
    }
    const timer = setTimeout(() => {
      if (!connected && !didInitialFetchRef.current) {
        didInitialFetchRef.current = true;
        getAlerts(MAX_ALERTS)
          .then((data) => setAlerts(data))
          .catch(() => {});
      }
    }, 3000); // Wait 3 seconds for WebSocket before falling back
    return () => clearTimeout(timer);
  }, [connected]);

  // ── Auto-dismiss timer ──
  useEffect(() => {
    const timer = setInterval(() => {
      const cutoff = Date.now() - AUTO_DISMISS_MS;
      setAlerts((prev) =>
        prev.filter((a) => {
          try {
            const ts = 'timestamp' in a ? a.timestamp : (a as { start_time?: string }).start_time;
            if (!ts) return true; // Keep if no timestamp
            const alertTime = new Date(ts).getTime();
            if (isNaN(alertTime)) return true; // Keep if invalid timestamp
            return alertTime > cutoff;
          } catch {
            return true; // Keep on error
          }
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
  const filteredAlerts = useMemo(() => {
    return alerts.filter((a) => {
      if (severityFilter !== 'all' && a.severity !== severityFilter) return false;
      if (typeFilter !== 'all' && a.type !== typeFilter) return false;
      return true;
    });
  }, [alerts, severityFilter, typeFilter]);

  // ── Render ──
  return (
    <div className="card">
      {/* ── Header ── */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mb-4">
        <h2 className="flex items-center gap-2" style={{ color: '#ff4444', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase' }}>
          <AlertTriangle className="h-5 w-5" style={{ color: '#ff4444' }} />
          ALERTS
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
                <Wifi className="h-3 w-3" style={{ color: '#00ff88' }} />
                <span className="hidden sm:inline" style={{ color: '#00ff88', fontFamily: 'monospace' }}>LIVE</span>
              </>
            ) : (
              <>
                <WifiOff className="h-3 w-3" style={{ color: '#ff4444' }} />
                <span className="hidden sm:inline" style={{ color: '#ff4444', fontFamily: 'monospace' }}>
                  {wsDisconnected ? 'POLLING' : 'CONNECTING'}
                </span>
              </>
            )}
          </span>

          {/* Sound toggle */}
          <button
            onClick={toggleSound}
            className="p-1 rounded transition-colors"
            style={{ color: soundOn ? '#00ff88' : 'rgba(0,255,136,0.3)' }}
            aria-label={soundOn ? 'Mute alert sounds' : 'Enable alert sounds'}
            title={soundOn ? 'Sound on' : 'Sound off'}
          >
            {soundOn ? (
              <Bell className="h-3.5 w-3.5" />
            ) : (
              <BellOff className="h-3.5 w-3.5" />
            )}
          </button>

          {/* Clear all */}
          {alerts.length > 0 && (
            <button
              onClick={handleClearAll}
              className="p-1 rounded transition-colors"
              style={{ color: 'rgba(0,255,136,0.4)' }}
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
          <Filter className="h-3 w-3" style={{ color: 'rgba(0,255,136,0.4)' }} />
          <span className="text-[11px] mr-1" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace', textTransform: 'uppercase' }}>Severity:</span>
          {SEVERITY_OPTIONS.map((s) => (
            <button
              key={s}
              onClick={() => setSeverityFilter(s)}
              className="px-2 py-0.5 rounded-full text-[11px] font-medium transition-all"
              style={severityFilter === s
                ? { background: 'rgba(0,255,136,0.15)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.3)', fontFamily: 'monospace' }
                : { background: 'rgba(0,255,136,0.05)', color: 'rgba(0,255,136,0.4)', border: '1px solid transparent', fontFamily: 'monospace' }
              }
            >
              {s === 'all' ? 'ALL' : s.toUpperCase()}
            </button>
          ))}
        </div>

        {/* Type filter */}
        <div className="flex items-center gap-1">
          <span className="text-[11px] mr-1" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace', textTransform: 'uppercase' }}>Type:</span>
          {TYPE_OPTIONS.map((t) => (
            <button
              key={t}
              onClick={() => setTypeFilter(t)}
              className="px-2 py-0.5 rounded-full text-[11px] font-medium transition-all"
              style={typeFilter === t
                ? { background: 'rgba(0,255,136,0.15)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.3)', fontFamily: 'monospace' }
                : { background: 'rgba(0,255,136,0.05)', color: 'rgba(0,255,136,0.4)', border: '1px solid transparent', fontFamily: 'monospace' }
              }
            >
              {t === 'all' ? 'ALL' : t === 'spike' ? 'SPIKE' : 'EDIT WAR'}
            </button>
          ))}
        </div>
      </div>

      {/* ── Alert list ── */}
      <div className="space-y-2 max-h-[500px] overflow-y-auto scrollbar-thin" role="log" aria-label="Alert notifications" aria-live="polite">
        {filteredAlerts.length > 0 ? (
          filteredAlerts.map((alert, index) => (
            <AlertCard
              key={`${alert.page_title}-${alert.type}-${index}`}
              alert={alert}
              onDismiss={handleDismiss}
            />
          ))
        ) : alerts.length > 0 ? (
          <div className="text-center py-8 text-sm" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
            <Filter className="h-6 w-6 mx-auto mb-2" style={{ color: 'rgba(0,255,136,0.2)' }} />
            NO ALERTS MATCH FILTERS
          </div>
        ) : (
          <div className="text-center py-8 text-sm" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
            <AlertTriangle className="h-8 w-8 mx-auto mb-2" style={{ color: 'rgba(0,255,136,0.2)' }} />
            MONITORING… NO ALERTS
          </div>
        )}
      </div>
    </div>
  );
});
