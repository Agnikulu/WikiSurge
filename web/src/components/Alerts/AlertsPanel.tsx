import { useCallback, useEffect, useRef, useState, useMemo, memo } from 'react';
import type { Alert } from '../../types';
import { getAlerts } from '../../utils/api';
import { useWebSocket } from '../../hooks/useWebSocket';
import { usePolling } from '../../hooks/usePolling';
import { buildWebSocketUrl, WS_ENDPOINTS } from '../../utils/websocket';
import { AlertCard } from './AlertCard';
import { useAppStore } from '../../store/appStore';
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

export const AlertsPanel = memo(function AlertsPanel({ showHistorical }: { showHistorical?: boolean } = {}) {
  // ── State ──
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [severityFilter, setSeverityFilter] = useState<string>('all');
  const [typeFilter, setTypeFilter] = useState<string>('all');
  const [soundOn, setSoundOn] = useState(isAlertSoundsEnabled());
  const alertsRef = useRef(alerts);
  alertsRef.current = alerts;
  
  // ── Global store ──
  const setAlertsCount = useAppStore((s) => s.setAlertsCount);
  
  // Update global alerts count whenever alerts change
  useEffect(() => {
    setAlertsCount(alerts.length);
  }, [alerts.length, setAlertsCount]);

  // ── WebSocket (primary data source) ──
  const wsUrl = buildWebSocketUrl(WS_ENDPOINTS.alerts);

  const handleWsMessage = useCallback((data: unknown) => {
    try {
      // WebSocket sends: {type: 'spike', data: {id, type, timestamp, data: {page_title, severity, ...}}}
      // We need to unwrap and flatten the nested structure
      const wrapper = data as { type?: string; data?: Record<string, unknown> };
      
      if (!wrapper || typeof wrapper !== 'object') {
        return;
      }
      
      // Check if it's the wrapped format
      if (wrapper.type && wrapper.data && typeof wrapper.data === 'object') {
        const outerData = wrapper.data as any;
        
        // Check if there's nested data (the actual alert fields)
        let alert: Alert;
        if (outerData.data && typeof outerData.data === 'object') {
          // Double-nested: flatten it
          alert = {
            ...outerData.data,
            ...outerData,
            type: wrapper.type,
          } as Alert;
          // Remove the nested data field to avoid confusion
          delete (alert as any).data;
        } else {
          // Single-level nesting
          alert = {
            ...outerData,
            type: wrapper.type,
          } as Alert;
        }
        
        // Validate required fields
        if (!alert.page_title || !alert.severity) {
          console.warn('[AlertsPanel] Missing required fields. Alert:', alert);
          return;
        }

        setAlerts((prev) => {
          // Deduplicate by page_title + type
          const key = `${alert.page_title}-${alert.type}`;
          const exists = prev.some(
            (a) => `${a.page_title}-${a.type}` === key,
          );
          if (exists) {
            return prev;
          }

          const updated = [alert, ...prev].slice(0, MAX_ALERTS);
          return updated;
        });

        // Sound notifications
        if (alert.severity === 'critical') playCriticalAlert();
        else if (alert.type === 'edit_war') playEditWarAlert();
      }
    } catch (error) {
      console.error('[AlertsPanel] Error handling WebSocket message:', error);
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

  // ── REST fallback polling (when WS disconnected for more than 5 seconds) ──
  const wsDisconnected = connectionState === 'error' || connectionState === 'disconnected';
  const [persistentDisconnect, setPersistentDisconnect] = useState(false);

  useEffect(() => {
    if (!wsDisconnected) {
      setPersistentDisconnect(false);
      return;
    }
    
    // Only enable REST polling if disconnected for more than 5 seconds
    // This prevents clearing alerts during brief reconnection hiccups
    const timer = setTimeout(() => {
      setPersistentDisconnect(true);
    }, 5000);
    
    return () => clearTimeout(timer);
  }, [wsDisconnected]);

  usePolling({
    fetcher: async () => {
      try {
        // For polling we only need active alerts (5 minutes)
        const since = new Date(Date.now() - AUTO_DISMISS_MS).toISOString();
        const data = await getAlerts(MAX_ALERTS, since);
        // Only update if we got data - don't clear existing alerts
        if (data && data.length > 0) {
          setAlerts((prev) => {
            // Merge with existing alerts, deduplicating by page_title + type
            const combined = [...data];
            const keys = new Set(data.map(a => `${a.page_title}-${a.type}`));
            
            // Add existing alerts that aren't in the new data
            for (const alert of prev) {
              const key = `${alert.page_title}-${alert.type}`;
              if (!keys.has(key)) {
                combined.push(alert);
                keys.add(key);
              }
            }
            
            return combined.slice(0, MAX_ALERTS);
          });
        }
      } catch {
        // silent – will retry next cycle
      }
    },
    interval: 10_000,
    enabled: persistentDisconnect,
  });

  // Initial fetch on mount to load alerts
  // WebSocket only provides NEW alerts, so we need REST to get existing ones
  const didInitialFetchRef = useRef(false);
  // Historical alerts handled by HistoricalAlerts component

  useEffect(() => {
    if (didInitialFetchRef.current) return;
    didInitialFetchRef.current = true;

    // Dashboard (no historical): fetch only active alerts (last 5 minutes)
    if (!showHistorical) {
      const since = new Date(Date.now() - AUTO_DISMISS_MS).toISOString();
      getAlerts(MAX_ALERTS, since)
        .then((data) => {
          if (data && data.length > 0) {
            setAlerts(data);
          }
        })
        .catch((err) => {
          console.error('[AlertsPanel] Initial fetch error:', err);
        });
      return;
    }

    // Alerts page: fetch active (5m)
    (async () => {
      try {
        const activeSince = new Date(Date.now() - AUTO_DISMISS_MS).toISOString();
        const active = await getAlerts(MAX_ALERTS, activeSince);
        if (active) setAlerts(active);
      } catch (err) {
        console.error('[AlertsPanel] Initial fetch error:', err);
      }
    })();
  }, [showHistorical]);

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
    const filtered = alerts.filter((a) => {
      if (severityFilter !== 'all' && a.severity !== severityFilter) return false;
      if (typeFilter !== 'all' && a.type !== typeFilter) return false;
      return true;
    });
    return filtered;
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
        {
          filteredAlerts.length > 0
            ? filteredAlerts.map((alert, i) => {
                return (
                  <AlertCard
                    key={`${alert.page_title}-${alert.type}-${i}`}
                    alert={alert}
                    onDismiss={handleDismiss}
                  />
                );
              })
            : alerts.length > 0
              ? (
                <div className="text-center py-8 text-sm" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
                  <Filter className="h-6 w-6 mx-auto mb-2" style={{ color: 'rgba(0,255,136,0.2)' }} />
                  NO ALERTS MATCH FILTERS
                </div>
              )
              : (
                <div className="text-center py-8 text-sm" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
                  <AlertTriangle className="h-8 w-8 mx-auto mb-2" style={{ color: 'rgba(0,255,136,0.2)' }} />
                  MONITORING… NO ALERTS
                </div>
              )
        }
      </div>
      {/* Historical alerts removed from Alerts page (kept in backend) */}
    </div>
  );
});
