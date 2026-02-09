import { useState, useCallback } from 'react';
import type { Edit, SearchParams } from '../../types';
import { searchEdits } from '../../utils/api';
import { SearchInput } from './SearchInput';
import { SearchResults } from './SearchResults';
import { AdvancedSearch } from './AdvancedSearch';
import { SlidersHorizontal, X } from 'lucide-react';

const PER_PAGE = 50;

type SortOption = 'relevance' | 'date_desc' | 'date_asc';

export function SearchInterface() {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<Edit[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [total, setTotal] = useState(0);
  const [searched, setSearched] = useState(false);
  const [page, setPage] = useState(1);
  const [sortBy, setSortBy] = useState<SortOption>('relevance');
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [advancedFilters, setAdvancedFilters] = useState<Partial<SearchParams>>({});

  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));

  const executeSearch = useCallback(
    async (q: string, pageNum: number, sort: SortOption, filters: Partial<SearchParams>) => {
      if (!q.trim()) return;

      setLoading(true);
      setError(null);
      setSearched(true);
      setQuery(q);

      try {
        const data = await searchEdits(q.trim(), PER_PAGE, {
          offset: (pageNum - 1) * PER_PAGE,
          sort,
          ...filters,
        });
        setResults(data.edits || []);
        setTotal(data.total || 0);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Search failed');
        setResults([]);
        setTotal(0);
      } finally {
        setLoading(false);
      }
    },
    []
  );

  const handleSearch = useCallback(
    (q: string) => {
      setPage(1);
      executeSearch(q, 1, sortBy, advancedFilters);
    },
    [executeSearch, sortBy, advancedFilters]
  );

  const handlePageChange = useCallback(
    (newPage: number) => {
      setPage(newPage);
      executeSearch(query, newPage, sortBy, advancedFilters);
      // Scroll to top of results
      window.scrollTo({ top: 0, behavior: 'smooth' });
    },
    [executeSearch, query, sortBy, advancedFilters]
  );

  const handleSortChange = useCallback(
    (sort: SortOption) => {
      setSortBy(sort);
      setPage(1);
      if (query.trim()) {
        executeSearch(query, 1, sort, advancedFilters);
      }
    },
    [executeSearch, query, advancedFilters]
  );

  const handleAdvancedApply = useCallback(
    (filters: Partial<SearchParams>) => {
      setAdvancedFilters(filters);
      if (filters.sort) {
        setSortBy(filters.sort);
      }
      setPage(1);
      if (query.trim()) {
        executeSearch(query, 1, filters.sort || sortBy, filters);
      }
    },
    [executeSearch, query, sortBy]
  );

  const handleClearFilters = () => {
    setAdvancedFilters({});
    setSortBy('relevance');
    setPage(1);
    if (query.trim()) {
      executeSearch(query, 1, 'relevance', {});
    }
  };

  const hasActiveFilters = Object.keys(advancedFilters).filter(
    (k) => k !== 'sort'
  ).length > 0;

  return (
    <div className="card" role="search" aria-label="Search edits">
      <div className="flex items-center justify-between mb-5">
        <h2 style={{ color: 'rgba(0,255,136,0.7)', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em', textTransform: 'uppercase' as const }}>SEARCH EDITS</h2>
        <button
          onClick={() => setAdvancedOpen(true)}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg transition-colors"
          style={hasActiveFilters
            ? { background: 'rgba(0,255,136,0.15)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.3)', fontFamily: 'monospace' }
            : { color: 'rgba(0,255,136,0.4)', background: 'rgba(0,255,136,0.05)', border: '1px solid transparent', fontFamily: 'monospace' }
          }
        >
          <SlidersHorizontal className="h-3.5 w-3.5" />
          ADVANCED
          {hasActiveFilters && (
            <span className="flex items-center justify-center w-4 h-4 rounded-full text-[10px] font-bold" style={{ background: '#00ff88', color: '#0a0f1a' }}>
              {Object.keys(advancedFilters).filter((k) => k !== 'sort').length}
            </span>
          )}
        </button>
      </div>

      {/* Search Input */}
      <SearchInput onSearch={handleSearch} loading={loading} initialQuery={query} />

      {/* Active filters indicator */}
      {hasActiveFilters && (
        <div className="flex items-center gap-2 mt-3 flex-wrap">
          <span className="text-xs" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>FILTERS:</span>
          {advancedFilters.language && (
            <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs" style={{ background: 'rgba(0,221,255,0.1)', color: '#00ddff', border: '1px solid rgba(0,221,255,0.2)', fontFamily: 'monospace' }}>
              {advancedFilters.language}wiki
            </span>
          )}
          {advancedFilters.user && (
            <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs" style={{ background: 'rgba(0,255,136,0.1)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.2)', fontFamily: 'monospace' }}>
              user: {advancedFilters.user}
            </span>
          )}
          {advancedFilters.exclude_bots && (
            <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs" style={{ background: 'rgba(255,170,0,0.1)', color: '#ffaa00', border: '1px solid rgba(255,170,0,0.2)', fontFamily: 'monospace' }}>
              no bots
            </span>
          )}
          {advancedFilters.from && (
            <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs" style={{ background: 'rgba(0,255,136,0.1)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.2)', fontFamily: 'monospace' }}>
              from: {advancedFilters.from}
            </span>
          )}
          {advancedFilters.to && (
            <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs" style={{ background: 'rgba(0,255,136,0.1)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.2)', fontFamily: 'monospace' }}>
              to: {advancedFilters.to}
            </span>
          )}
          <button
            onClick={handleClearFilters}
            className="inline-flex items-center gap-0.5 px-2 py-0.5 rounded-full text-xs transition-colors"
            style={{ color: '#ff4444', fontFamily: 'monospace' }}
          >
            <X className="h-3 w-3" />
            CLEAR
          </button>
        </div>
      )}

      {/* Results */}
      <SearchResults
        results={results}
        total={total}
        loading={loading}
        error={error}
        searched={searched}
        query={query}
        currentPage={page}
        totalPages={totalPages}
        sortBy={sortBy}
        onPageChange={handlePageChange}
        onSortChange={handleSortChange}
        perPage={PER_PAGE}
      />

      {/* Advanced Search Modal */}
      <AdvancedSearch
        isOpen={advancedOpen}
        onClose={() => setAdvancedOpen(false)}
        onApply={handleAdvancedApply}
        currentFilters={{ ...advancedFilters, sort: sortBy }}
      />
    </div>
  );
}
