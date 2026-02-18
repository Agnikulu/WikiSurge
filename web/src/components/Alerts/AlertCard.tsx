import type { Alert, SpikeAlert, EditWarAlert } from '../../types';
import { formatTimestamp, getSeverityColor, truncateTitle, buildWikiUrl } from '../../utils/formatting';
import { Zap, Swords, X } from 'lucide-react';
import { SeverityBadge } from './SeverityBadge';

interface AlertCardProps {
  alert: Alert;
  onDismiss?: (alert: Alert) => void;
}

export function AlertCard({ alert, onDismiss }: AlertCardProps) {
  // Defensive: ensure alert has required fields
  if (!alert || !alert.type || !alert.page_title || !alert.severity) {
    console.warn('[AlertCard] Invalid alert data:', alert);
    return null;
  }

  const severity = getSeverityColor(alert.severity);

  if (alert.type === 'spike') {
    return (
      <SpikeAlertCard alert={alert} severity={severity} onDismiss={onDismiss} />
    );
  }

  return (
    <EditWarAlertCard
      alert={alert as EditWarAlert}
      severity={severity}
      onDismiss={onDismiss}
    />
  );
}

function SpikeAlertCard({
  alert,
  severity,
  onDismiss,
}: {
  alert: SpikeAlert;
  severity: ReturnType<typeof getSeverityColor>;
  onDismiss?: (alert: Alert) => void;
}) {
  const wikiUrl = buildWikiUrl(alert.page_title, alert.server_url);

  return (
    <div
      className={`
        relative p-3 rounded-lg border-l-4 ${severity.bg} ${severity.border}
        animate-slide-up transition-all duration-200
        hover:shadow-sm group
      `}
    >
      {/* Dismiss */}
      {onDismiss && (
        <button
          onClick={() => onDismiss(alert)}
          className="absolute top-2 right-2 p-0.5 rounded opacity-0 group-hover:opacity-100 transition-opacity"
          style={{ color: 'rgba(0,255,136,0.4)' }}
          aria-label="Dismiss alert"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}

      <div className="flex items-start gap-2">
        {/* Icon */}
        <span className="mt-0.5 text-base" role="img" aria-label="spike">
          {alert.severity === 'critical' ? '🔴' : '⚠️'}
        </span>

        <div className="flex-1 min-w-0 pr-4">
          {/* Header row */}
          <div className="flex flex-wrap items-center gap-1.5 mb-0.5">
            <Zap className={`h-3.5 w-3.5 ${severity.text}`} />
            <span className={`text-sm font-semibold ${severity.text}`} style={{ fontFamily: 'monospace', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Spike Detected</span>
            <SeverityBadge severity={alert.severity} />
            {/* historical tag removed; historical alerts shown in separate section on Alerts page */}
          </div>

          {/* Page title */}
          <a
            href={wikiUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-sm font-medium truncate block"
            style={{ color: '#00ff88', fontFamily: 'monospace' }}
            title={alert.page_title}
          >
            {truncateTitle(alert.page_title, 50)}
          </a>

          {/* Stats */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 mt-1 text-xs" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}>
            {alert.spike_ratio != null && (
              <>
                <span className="font-medium" style={{ color: '#ffaa00' }}>
                  {alert.spike_ratio.toFixed(1)}x normal rate
                </span>
                <span>·</span>
              </>
            )}
            {alert.edits_5min != null && (
              <>
                <span>{alert.edits_5min} edits in 5 min</span>
                <span>·</span>
              </>
            )}
            <span>{formatTimestamp(alert.timestamp)}</span>
          </div>
        </div>
      </div>
    </div>
  );
}

function EditWarAlertCard({
  alert,
  severity,
  onDismiss,
}: {
  alert: EditWarAlert;
  severity: ReturnType<typeof getSeverityColor>;
  onDismiss?: (alert: Alert) => void;
}) {
  const wikiUrl = buildWikiUrl(alert.page_title, alert.server_url);

  return (
    <div
      className={`
        relative p-3 rounded-lg border-l-4 ${severity.bg} ${severity.border}
        animate-slide-up transition-all duration-200
        hover:shadow-sm group
      `}
    >
      {/* Dismiss */}
      {onDismiss && (
        <button
          onClick={() => onDismiss(alert)}
          className="absolute top-2 right-2 p-0.5 rounded opacity-0 group-hover:opacity-100 transition-opacity"
          style={{ color: 'rgba(0,255,136,0.4)' }}
          aria-label="Dismiss alert"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}

      <div className="flex items-start gap-2">
        {/* Icon */}
        <span className="mt-0.5 text-base" role="img" aria-label="edit war">
          ⚔️
        </span>

        <div className="flex-1 min-w-0 pr-4">
          {/* Header row */}
          <div className="flex flex-wrap items-center gap-1.5 mb-0.5">
            <Swords className={`h-3.5 w-3.5 ${severity.text}`} />
            <span className={`text-sm font-semibold ${severity.text}`} style={{ fontFamily: 'monospace', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Edit War</span>
            <SeverityBadge severity={alert.severity} />
            {/* historical tag removed; historical alerts shown in separate section on Alerts page */}
          </div>

          {/* Page title */}
          <a
            href={wikiUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-sm font-medium truncate block"
            style={{ color: '#00ff88', fontFamily: 'monospace' }}
            title={alert.page_title}
          >
            {truncateTitle(alert.page_title, 50)}
          </a>

          {/* Stats */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 mt-1 text-xs" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}>
            {alert.editor_count != null && (
              <>
                <span>{alert.editor_count} editors</span>
                <span>·</span>
              </>
            )}
            {alert.revert_count != null && (
              <>
                <span>{alert.revert_count} reverts</span>
                <span>·</span>
              </>
            )}
            {alert.edit_count != null && (
              <>
                <span>{alert.edit_count} edits</span>
                <span>·</span>
              </>
            )}
            <span>{formatTimestamp(alert.start_time)}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
