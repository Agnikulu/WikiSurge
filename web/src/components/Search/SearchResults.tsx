import type { Edit } from '../../types';
import { SearchResultItem } from './SearchResultItem';
import { Pagination } from './Pagination';
import { exportSearchResults } from '../../utils/export';
import { SearchX, AlertTriangle, Search, Download, ArrowUpDown } from 'lucide-react';

type SortOption = 'relevance' | 'date_desc' | 'date_asc';

interface SearchResultsProps {
  results: Edit[];
  total: number;
  loading: boolean;
  error: string | null;
  searched: boolean;
  query: string;
  currentPage: number;
  totalPages: number;
  sortBy: SortOption;
  onPageChange: (page: number) => void;
  onSortChange: (sort: SortOption) => void;
  perPage: number;
}

export function SearchResults({
  results,
  total,
  loading,
  error,
  searched,
  query,
  currentPage,
  totalPages,
  sortBy,
  onPageChange,
  onSortChange,
  perPage,
}: SearchResultsProps) {
  // Loading skeleton
  if (loading) {
    return (
      <div className="space-y-3 mt-6">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="animate-pulse p-4 rounded-lg" style={{ border: '1px solid rgba(0,255,136,0.08)' }}>
            <div className="flex items-start gap-3">
              <div className="w-8 h-8 rounded-full flex-shrink-0" style={{ background: 'rgba(0,255,136,0.06)' }} />
              <div className="flex-1 space-y-2">
                <div className="h-4 rounded w-2/3" style={{ background: 'rgba(0,255,136,0.06)' }} />
                <div className="h-3 rounded w-1/2" style={{ background: 'rgba(0,255,136,0.04)' }} />
                <div className="h-3 rounded w-3/4" style={{ background: 'rgba(0,255,136,0.04)' }} />
              </div>
              <div className="h-4 rounded w-12" style={{ background: 'rgba(0,255,136,0.06)' }} />
            </div>
          </div>
        ))}
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center">
        <AlertTriangle className="h-10 w-10 mb-3" style={{ color: '#ff4444' }} />
        <p className="text-sm font-medium" style={{ color: '#ff4444', fontFamily: 'monospace' }}>SEARCH FAILED</p>
        <p className="text-xs mt-1" style={{ color: 'rgba(0,255,136,0.3)', fontFamily: 'monospace' }}>{error}</p>
      </div>
    );
  }

  // Initial state (not searched yet)
  if (!searched) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <Search className="h-12 w-12 mb-4" style={{ color: 'rgba(0,255,136,0.15)' }} />
        <p className="text-sm" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}>ENTER A SEARCH QUERY TO FIND EDITS</p>
        <p className="text-xs mt-1" style={{ color: 'rgba(0,255,136,0.3)', fontFamily: 'monospace' }}>Search by page title, user name, or comment</p>
      </div>
    );
  }

  // No results
  if (results.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center">
        <SearchX className="h-10 w-10 mb-3" style={{ color: 'rgba(0,255,136,0.15)' }} />
        <p className="text-sm" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}>
          NO RESULTS FOR &ldquo;<span style={{ color: '#00ff88' }}>{query}</span>&rdquo;
        </p>
        <p className="text-xs mt-1" style={{ color: 'rgba(0,255,136,0.3)', fontFamily: 'monospace' }}>Try different keywords or adjust your filters</p>
      </div>
    );
  }

  // Results
  const startIdx = (currentPage - 1) * perPage + 1;
  const endIdx = Math.min(currentPage * perPage, total);

  return (
    <div className="mt-6">
      {/* Results header */}
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <p className="text-sm" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}>
          SHOWING <span style={{ color: '#00ff88', fontWeight: 600 }}>{startIdx}–{endIdx}</span> OF{' '}
          <span style={{ color: '#00ff88', fontWeight: 600 }}>{total.toLocaleString()}</span> RESULTS
        </p>

        <div className="flex items-center gap-3">
          {/* Sort selector */}
          <div className="flex items-center gap-1.5">
            <ArrowUpDown className="h-3.5 w-3.5" style={{ color: 'rgba(0,255,136,0.3)' }} />
            <select
              value={sortBy}
              onChange={(e) => onSortChange(e.target.value as SortOption)}
              className="text-sm border-0 bg-transparent focus:ring-0 cursor-pointer py-0 pr-6"
              style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}
              aria-label="Sort results"
            >
              <option value="relevance">Relevance</option>
              <option value="date_desc">Newest first</option>
              <option value="date_asc">Oldest first</option>
            </select>
          </div>

          {/* Export button */}
          <button
            onClick={() => exportSearchResults(results)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg transition-colors"
            style={{ background: 'rgba(0,255,136,0.1)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.2)', fontFamily: 'monospace' }}
            aria-label="Export results as CSV"
          >
            <Download className="h-3.5 w-3.5" />
            EXPORT CSV
          </button>
        </div>
      </div>

      {/* Results list */}
      <div className="space-y-2">
        {results.map((edit, index) => (
          <SearchResultItem
            key={`${edit.id}-${index}`}
            edit={edit}
            query={query}
          />
        ))}
      </div>

      {/* Pagination */}
      <Pagination
        currentPage={currentPage}
        totalPages={totalPages}
        onPageChange={onPageChange}
      />
    </div>
  );
}
