import { useCallback } from 'react';
import type { Alert } from '../../types';
import { getAlerts } from '../../utils/api';
import { useAPI } from '../../hooks/useAPI';
import { usePolling } from '../../hooks/usePolling';
import { AlertCard } from './AlertCard';
import { AlertTriangle } from 'lucide-react';

export function AlertsPanel() {
  const fetcher = useCallback(() => getAlerts(20), []);

  const { data: alerts, loading, error, refetch } = useAPI<Alert[]>({
    fetcher,
    initialData: [],
  });

  // Poll every 10 seconds
  usePolling({
    fetcher: refetch,
    interval: 10000,
  });

  if (loading && (!alerts || alerts.length === 0)) {
    return (
      <div className="card">
        <h2 className="text-lg font-semibold text-gray-900 mb-4 flex items-center space-x-2">
          <AlertTriangle className="h-5 w-5 text-orange-500" />
          <span>Alerts</span>
        </h2>
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="animate-pulse">
              <div className="h-4 bg-gray-200 rounded w-3/4 mb-1" />
              <div className="h-3 bg-gray-200 rounded w-1/2" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="card">
        <h2 className="text-lg font-semibold text-gray-900 mb-4 flex items-center space-x-2">
          <AlertTriangle className="h-5 w-5 text-orange-500" />
          <span>Alerts</span>
        </h2>
        <p className="text-sm text-gray-500 text-center">Failed to load alerts</p>
        <button onClick={refetch} className="text-primary-600 hover:underline text-sm mt-1 mx-auto block">
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="card">
      <h2 className="text-lg font-semibold text-gray-900 mb-4 flex items-center space-x-2">
        <AlertTriangle className="h-5 w-5 text-orange-500" />
        <span>Alerts</span>
        {alerts && alerts.length > 0 && (
          <span className="badge badge-critical">{alerts.length}</span>
        )}
      </h2>
      <div className="space-y-2 max-h-96 overflow-y-auto">
        {alerts && alerts.length > 0 ? (
          alerts.map((alert, index) => (
            <AlertCard key={`${alert.page_title}-${index}`} alert={alert} />
          ))
        ) : (
          <p className="text-sm text-gray-400 text-center py-4">No active alerts</p>
        )}
      </div>
    </div>
  );
}
