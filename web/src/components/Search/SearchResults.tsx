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
          <div key={i} className="animate-pulse p-4 rounded-lg border border-gray-100">
            <div className="flex items-start gap-3">
              <div className="w-8 h-8 bg-gray-200 rounded-full flex-shrink-0" />
              <div className="flex-1 space-y-2">
                <div className="h-4 bg-gray-200 rounded w-2/3" />
                <div className="h-3 bg-gray-200 rounded w-1/2" />
                <div className="h-3 bg-gray-200 rounded w-3/4" />
              </div>
              <div className="h-4 bg-gray-200 rounded w-12" />
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
        <AlertTriangle className="h-10 w-10 text-red-400 mb-3" />
        <p className="text-sm font-medium text-red-600">Search failed. Please try again.</p>
        <p className="text-xs text-gray-400 mt-1">{error}</p>
      </div>
    );
  }

  // Initial state (not searched yet)
  if (!searched) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <Search className="h-12 w-12 text-gray-300 mb-4" />
        <p className="text-sm text-gray-500">Enter a search query to find edits</p>
        <p className="text-xs text-gray-400 mt-1">Search by page title, user name, or comment</p>
      </div>
    );
  }

  // No results
  if (results.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center">
        <SearchX className="h-10 w-10 text-gray-300 mb-3" />
        <p className="text-sm text-gray-500">
          No edits found for &ldquo;<span className="font-medium text-gray-700">{query}</span>&rdquo;
        </p>
        <p className="text-xs text-gray-400 mt-1">Try different keywords or adjust your filters</p>
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
        <p className="text-sm text-gray-600">
          Showing <span className="font-semibold">{startIdx}–{endIdx}</span> of{' '}
          <span className="font-semibold">{total.toLocaleString()}</span> results
        </p>

        <div className="flex items-center gap-3">
          {/* Sort selector */}
          <div className="flex items-center gap-1.5">
            <ArrowUpDown className="h-3.5 w-3.5 text-gray-400" />
            <select
              value={sortBy}
              onChange={(e) => onSortChange(e.target.value as SortOption)}
              className="text-sm border-0 bg-transparent text-gray-600 focus:ring-0 cursor-pointer py-0 pr-6"
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
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-gray-600 bg-gray-100 hover:bg-gray-200 rounded-lg transition-colors"
            aria-label="Export results as CSV"
          >
            <Download className="h-3.5 w-3.5" />
            Export CSV
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
