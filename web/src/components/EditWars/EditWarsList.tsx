import { useCallback } from 'react';
import type { EditWar } from '../../types';
import { getEditWars } from '../../utils/api';
import { useAPI } from '../../hooks/useAPI';
import { usePolling } from '../../hooks/usePolling';
import { formatTimestamp, truncateTitle } from '../../utils/formatting';
import { Swords, Users, RotateCcw } from 'lucide-react';
import { getSeverityColor } from '../../utils/formatting';

export function EditWarsList() {
  const fetcher = useCallback(() => getEditWars(true), []);

  const { data: editWars, loading, error, refetch } = useAPI<EditWar[]>({
    fetcher,
    initialData: [],
  });

  // Poll every 30 seconds
  usePolling({
    fetcher: refetch,
    interval: 30000,
  });

  if (loading && (!editWars || editWars.length === 0)) {
    return (
      <div className="card">
        <h2 className="text-lg font-semibold text-gray-900 mb-4 flex items-center space-x-2">
          <Swords className="h-5 w-5 text-red-500" />
          <span>Active Edit Wars</span>
        </h2>
        <div className="animate-pulse space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i}>
              <div className="h-4 bg-gray-200 rounded w-2/3 mb-1" />
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
          <Swords className="h-5 w-5 text-red-500" />
          <span>Active Edit Wars</span>
        </h2>
        <p className="text-sm text-gray-500 text-center">Failed to load edit wars</p>
        <button onClick={refetch} className="text-primary-600 hover:underline text-sm mt-1 mx-auto block">
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="card">
      <h2 className="text-lg font-semibold text-gray-900 mb-4 flex items-center space-x-2">
        <Swords className="h-5 w-5 text-red-500" />
        <span>Active Edit Wars</span>
        {editWars && editWars.length > 0 && (
          <span className="badge badge-critical">{editWars.length}</span>
        )}
      </h2>

      <div className="space-y-3 max-h-96 overflow-y-auto">
        {editWars && editWars.length > 0 ? (
          editWars.map((war, index) => {
            const severity = getSeverityColor(war.severity);
            return (
              <div
                key={`${war.page_title}-${index}`}
                className={`p-3 rounded-lg border ${severity.bg} ${severity.border}`}
              >
                <p className="font-medium text-gray-900 text-sm" title={war.page_title}>
                  {truncateTitle(war.page_title, 60)}
                </p>
                <div className="flex items-center space-x-4 mt-2 text-xs text-gray-600">
                  <span className="flex items-center space-x-1">
                    <Users className="h-3 w-3" />
                    <span>{war.editors.length} editors</span>
                  </span>
                  <span className="flex items-center space-x-1">
                    <RotateCcw className="h-3 w-3" />
                    <span>{war.revert_count} reverts</span>
                  </span>
                  <span>{war.edit_count} edits</span>
                  <span>·</span>
                  <span>{formatTimestamp(war.start_time)}</span>
                </div>
              </div>
            );
          })
        ) : (
          <p className="text-sm text-gray-400 text-center py-4">No active edit wars</p>
        )}
      </div>
    </div>
  );
}
