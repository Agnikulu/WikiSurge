import type { Alert, SpikeAlert, EditWarAlert } from '../../types';
import { formatTimestamp, getSeverityColor, truncateTitle } from '../../utils/formatting';
import { Zap, Swords } from 'lucide-react';

interface AlertCardProps {
  alert: Alert;
}

export function AlertCard({ alert }: AlertCardProps) {
  const severity = getSeverityColor(alert.severity);

  if (alert.type === 'spike') {
    return <SpikeAlertCard alert={alert} severity={severity} />;
  }

  return <EditWarAlertCard alert={alert as EditWarAlert} severity={severity} />;
}

function SpikeAlertCard({
  alert,
  severity,
}: {
  alert: SpikeAlert;
  severity: ReturnType<typeof getSeverityColor>;
}) {
  return (
    <div className={`p-3 rounded-lg border ${severity.bg} ${severity.border}`}>
      <div className="flex items-start space-x-2">
        <Zap className={`h-4 w-4 mt-0.5 ${severity.text}`} />
        <div className="flex-1 min-w-0">
          <div className="flex items-center space-x-2">
            <p className={`text-sm font-medium ${severity.text}`}>Spike Detected</p>
            <span className={`badge ${severity.bg} ${severity.text}`}>
              {alert.severity}
            </span>
          </div>
          <p className="text-sm text-gray-700 truncate mt-0.5" title={alert.page_title}>
            {truncateTitle(alert.page_title, 50)}
          </p>
          <div className="flex items-center space-x-3 mt-1 text-xs text-gray-500">
            <span>{alert.spike_ratio.toFixed(1)}x normal</span>
            <span>·</span>
            <span>{alert.edits_5min} edits/5min</span>
            <span>·</span>
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
}: {
  alert: EditWarAlert;
  severity: ReturnType<typeof getSeverityColor>;
}) {
  return (
    <div className={`p-3 rounded-lg border ${severity.bg} ${severity.border}`}>
      <div className="flex items-start space-x-2">
        <Swords className={`h-4 w-4 mt-0.5 ${severity.text}`} />
        <div className="flex-1 min-w-0">
          <div className="flex items-center space-x-2">
            <p className={`text-sm font-medium ${severity.text}`}>Edit War</p>
            <span className={`badge ${severity.bg} ${severity.text}`}>
              {alert.severity}
            </span>
          </div>
          <p className="text-sm text-gray-700 truncate mt-0.5" title={alert.page_title}>
            {truncateTitle(alert.page_title, 50)}
          </p>
          <div className="flex items-center space-x-3 mt-1 text-xs text-gray-500">
            <span>{alert.editor_count} editors</span>
            <span>·</span>
            <span>{alert.revert_count} reverts</span>
            <span>·</span>
            <span>{formatTimestamp(alert.start_time)}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
