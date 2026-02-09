import { useCallback, useState } from 'react';
import type { EditWar } from '../../types';
import { getEditWars } from '../../utils/api';
import { useAPI } from '../../hooks/useAPI';
import { EditWarCard } from './EditWarCard';
import {
  Calendar,
  ChevronLeft,
  ChevronRight,
  Download,
  Search,
  Swords,
} from 'lucide-react';
import { SeverityBadge } from '../Alerts/SeverityBadge';

const PAGE_SIZE = 10;

const SEVERITY_OPTIONS = ['all', 'critical', 'high', 'medium', 'low'] as const;

/**
 * Browse historical (resolved) edit wars with date-range filtering,
 * severity filtering, pagination, and CSV export.
 */
export function HistoricalEditWars() {
  const [page, setPage] = useState(0);
  const [severity, setSeverity] = useState<string>('all');
  const [pageSearch, setPageSearch] = useState('');

  // Fetch all (active=false includes resolved)
  const fetcher = useCallback(() => getEditWars(false), []);
  const { data: allWars, loading, error, refetch } = useAPI<EditWar[]>({
    fetcher,
    initialData: [],
  });

  // Client-side filtering
  const filtered = (allWars ?? [])
    .filter((w) => !w.active) // only resolved
    .filter((w) => severity === 'all' || w.severity.toLowerCase() === severity)
    .filter(
      (w) =>
        !pageSearch ||
        w.page_title.toLowerCase().includes(pageSearch.toLowerCase()),
    )
    .sort(
      (a, b) =>
        new Date(b.start_time).getTime() - new Date(a.start_time).getTime(),
    );

  const totalPages = Math.max(Math.ceil(filtered.length / PAGE_SIZE), 1);
  const paginated = filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE);

  // ── Export to CSV ──────────────────────────────────
  const handleExport = () => {
    const headers =
      'Page Title,Editors,Edit Count,Revert Count,Severity,Start Time,Last Edit\n';
    const rows = filtered
      .map(
        (w) =>
          `"${w.page_title}","${w.editors.join('; ')}",${w.edit_count},${w.revert_count},${w.severity},${w.start_time},${w.last_edit}`,
      )
      .join('\n');
    const csv = headers + rows;
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `edit-war-history-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="card">
      {/* Header */}
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <h2 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
          <Calendar className="h-5 w-5 text-gray-500 dark:text-gray-400" />
          Edit War History
          <span className="text-sm font-normal text-gray-500 dark:text-gray-400">
            ({filtered.length} result{filtered.length !== 1 ? 's' : ''})
          </span>
        </h2>

        <button
          onClick={handleExport}
          disabled={filtered.length === 0}
          className="inline-flex items-center gap-1 px-3 py-1.5 text-xs font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded disabled:opacity-50 transition-colors"
        >
          <Download className="h-3 w-3" />
          Export CSV
        </button>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-3 mb-4 pb-4 border-b border-gray-100 dark:border-gray-700">
        {/* Page search */}
        <div className="relative flex-1 min-w-[200px]">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-gray-400" />
          <input
            type="text"
            placeholder="Search page title..."
            value={pageSearch}
            onChange={(e) => {
              setPageSearch(e.target.value);
              setPage(0);
            }}
            className="w-full pl-8 pr-3 py-1.5 text-sm border border-gray-200 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:outline-none focus:ring-1 focus:ring-blue-400"
          />
        </div>

        {/* Severity filter */}
        <div className="flex items-center gap-1.5">
          {SEVERITY_OPTIONS.map((s) => (
            <button
              key={s}
              onClick={() => {
                setSeverity(s);
                setPage(0);
              }}
              className={`px-2 py-1 text-xs rounded transition-colors ${
                severity === s
                  ? 'bg-gray-200 dark:bg-gray-600 text-gray-800 dark:text-gray-200 font-medium'
                  : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
              }`}
            >
              {s === 'all' ? (
                'All'
              ) : (
                <SeverityBadge severity={s} />
              )}
            </button>
          ))}
        </div>
      </div>

      {/* Body */}
      {loading && filtered.length === 0 ? (
        <div className="animate-pulse space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="h-28 bg-gray-100 dark:bg-gray-700 rounded-lg" />
          ))}
        </div>
      ) : error ? (
        <div className="text-center py-8">
          <p className="text-sm text-gray-500 dark:text-gray-400">Failed to load history</p>
          <button
            onClick={refetch}
            className="text-blue-600 hover:underline text-sm mt-2"
          >
            Retry
          </button>
        </div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-8">
          <Swords className="h-10 w-10 text-gray-300 mx-auto mb-2" />
          <p className="text-sm text-gray-400">No historical edit wars found</p>
        </div>
      ) : (
        <>
          <div className="space-y-3">
            {paginated.map((war) => (
              <EditWarCard key={war.page_title} war={war} />
            ))}
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-4 pt-4 border-t border-gray-100 dark:border-gray-700">
              <button
                onClick={() => setPage((p) => Math.max(p - 1, 0))}
                disabled={page === 0}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-xs text-gray-600 dark:text-gray-400 border border-gray-200 dark:border-gray-700 rounded hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-40 transition-colors"
              >
                <ChevronLeft className="h-3 w-3" />
                Previous
              </button>

              <span className="text-xs text-gray-500 dark:text-gray-400">
                Page {page + 1} of {totalPages}
              </span>

              <button
                onClick={() =>
                  setPage((p) => Math.min(p + 1, totalPages - 1))
                }
                disabled={page >= totalPages - 1}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-xs text-gray-600 dark:text-gray-400 border border-gray-200 dark:border-gray-700 rounded hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-40 transition-colors"
              >
                Next
                <ChevronRight className="h-3 w-3" />
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
