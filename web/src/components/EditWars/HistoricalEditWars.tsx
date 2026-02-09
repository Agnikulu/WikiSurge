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
    .filter((w) => severity === 'all' || (w.severity?.toLowerCase() ?? '') === severity)
    .filter(
      (w) =>
        !pageSearch ||
        w.page_title.toLowerCase().includes(pageSearch.toLowerCase()),
    )
    .sort(
      (a, b) => {
        const ta = a.start_time ? new Date(a.start_time).getTime() : 0;
        const tb = b.start_time ? new Date(b.start_time).getTime() : 0;
        return tb - ta;
      },
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
        <h2 className="flex items-center gap-2" style={{ color: 'rgba(0,255,136,0.7)', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase' as const }}>
          <Calendar className="h-5 w-5" style={{ color: 'rgba(0,255,136,0.5)' }} />
          EDIT WAR HISTORY
          <span className="text-sm font-normal" style={{ color: 'rgba(0,255,136,0.4)' }}>
            ({filtered.length} result{filtered.length !== 1 ? 's' : ''})
          </span>
        </h2>

        <button
          onClick={handleExport}
          disabled={filtered.length === 0}
          className="inline-flex items-center gap-1 px-3 py-1.5 text-xs font-medium rounded disabled:opacity-50 transition-colors"
          style={{ background: 'rgba(0,255,136,0.1)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.2)', fontFamily: 'monospace' }}
        >
          <Download className="h-3 w-3" />
          EXPORT CSV
        </button>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-3 mb-4 pb-4" style={{ borderBottom: '1px solid rgba(0,255,136,0.1)' }}>
        {/* Page search */}
        <div className="relative flex-1 min-w-[200px]">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5" style={{ color: 'rgba(0,255,136,0.3)' }} />
          <input
            type="text"
            placeholder="SEARCH PAGE TITLE..."
            value={pageSearch}
            onChange={(e) => {
              setPageSearch(e.target.value);
              setPage(0);
            }}
            className="w-full pl-8 pr-3 py-1.5 text-sm rounded-md focus:outline-none focus:ring-1"
            style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }}
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
              className="px-2 py-1 text-xs rounded transition-colors"
              style={severity === s
                ? { background: 'rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }
                : { color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }
              }
            >
              {s === 'all' ? (
                'ALL'
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
            <div key={i} className="h-28 rounded-lg" style={{ background: 'rgba(0,255,136,0.04)' }} />
          ))}
        </div>
      ) : error ? (
        <div className="text-center py-8">
          <p className="text-sm" style={{ color: '#ff4444', fontFamily: 'monospace' }}>FAILED TO LOAD HISTORY</p>
          <button
            onClick={refetch}
            className="text-sm mt-2" style={{ color: '#00ff88', fontFamily: 'monospace' }}
          >
            RETRY
          </button>
        </div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-8">
          <Swords className="h-10 w-10 mx-auto mb-2" style={{ color: 'rgba(0,255,136,0.15)' }} />
          <p className="text-sm" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>NO HISTORICAL EDIT WARS FOUND</p>
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
            <div className="flex items-center justify-between mt-4 pt-4" style={{ borderTop: '1px solid rgba(0,255,136,0.1)' }}>
              <button
                onClick={() => setPage((p) => Math.max(p - 1, 0))}
                disabled={page === 0}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-xs rounded disabled:opacity-40 transition-colors"
                style={{ color: '#00ff88', border: '1px solid rgba(0,255,136,0.2)', fontFamily: 'monospace' }}
              >
                <ChevronLeft className="h-3 w-3" />
                PREV
              </button>

              <span className="text-xs" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
                PAGE {page + 1} OF {totalPages}
              </span>

              <button
                onClick={() =>
                  setPage((p) => Math.min(p + 1, totalPages - 1))
                }
                disabled={page >= totalPages - 1}
                className="inline-flex items-center gap-1 px-3 py-1.5 text-xs rounded disabled:opacity-40 transition-colors"
                style={{ color: '#00ff88', border: '1px solid rgba(0,255,136,0.2)', fontFamily: 'monospace' }}
              >
                NEXT
                <ChevronRight className="h-3 w-3" />
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
