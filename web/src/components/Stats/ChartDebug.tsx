import { useCallback } from 'react';
import { getStats } from '../../utils/api';
import { usePollingData } from '../../hooks/usePolling';
import type { Stats } from '../../types';

/**
 * Temporary diagnostic component – displays raw stats data as plain text
 * to verify the data pipeline works.  Remove once charts are confirmed.
 */
export function ChartDebug() {
  const fetchFn = useCallback(() => getStats(), []);
  const { data: stats, loading, error } = usePollingData<Stats>({
    fetchFunction: fetchFn,
    interval: 5_000,
  });

  return (
    <div
      style={{
        background: '#1e293b',
        color: '#e2e8f0',
        padding: '16px',
        borderRadius: '8px',
        fontFamily: 'monospace',
        fontSize: '13px',
        lineHeight: '1.6',
        marginBottom: '16px',
      }}
    >
      <strong style={{ color: '#facc15' }}>[Chart Debug Panel]</strong>
      <br />
      loading: {String(loading)}
      <br />
      error: {error ? error.message : 'none'}
      <br />
      stats: {stats ? 'present' : 'null'}
      <br />
      edits_per_second: {stats?.edits_per_second ?? '—'}
      <br />
      top_language: {stats?.top_language ?? '—'}
      <br />
      top_languages: {stats?.top_languages
        ? stats.top_languages.map((l) => `${l.language}(${l.count})`).join(', ')
        : '—'}
      <br />
      edits_by_type: {stats?.edits_by_type
        ? `human=${stats.edits_by_type.human}, bot=${stats.edits_by_type.bot}`
        : '—'}
    </div>
  );
}
