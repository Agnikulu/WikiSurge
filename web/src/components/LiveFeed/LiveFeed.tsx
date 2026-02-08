import type { Edit } from '../../types';
import { useWebSocket } from '../../hooks/useWebSocket';
import { buildWebSocketUrl, WS_ENDPOINTS } from '../../utils/websocket';
import { EditItem } from './EditItem';

export function LiveFeed() {
  const wsUrl = buildWebSocketUrl(WS_ENDPOINTS.edits);

  const { data: edits, connected } = useWebSocket<Edit>({
    url: wsUrl,
  });

  return (
    <div className="card">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-gray-900">Live Feed</h2>
        <div className="flex items-center space-x-2">
          <span
            className={`status-dot ${connected ? 'status-dot-connected' : 'status-dot-disconnected'}`}
          />
          <span className="text-xs text-gray-500">
            {connected ? 'Live' : 'Connecting...'}
          </span>
        </div>
      </div>

      <div className="space-y-2 max-h-96 overflow-y-auto">
        {edits.length === 0 ? (
          <p className="text-sm text-gray-400 text-center py-8">
            {connected ? 'Waiting for edits...' : 'Connecting to live feed...'}
          </p>
        ) : (
          edits.map((edit, index) => (
            <EditItem key={`${edit.id}-${index}`} edit={edit} />
          ))
        )}
      </div>
    </div>
  );
}
