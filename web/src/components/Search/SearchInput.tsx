import { useState, useEffect, useRef, useCallback } from 'react';
import { Search, Loader2, X } from 'lucide-react';

const RECENT_SEARCHES_KEY = 'wikisurge-recent-searches';
const MAX_RECENT = 8;

interface SearchInputProps {
  onSearch: (query: string) => void;
  loading: boolean;
  initialQuery?: string;
}

function getRecentSearches(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_SEARCHES_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch {
    return [];
  }
}

function saveRecentSearch(query: string): void {
  const recent = getRecentSearches().filter((s) => s !== query);
  recent.unshift(query);
  localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(recent.slice(0, MAX_RECENT)));
}

export function SearchInput({ onSearch, loading, initialQuery = '' }: SearchInputProps) {
  const [query, setQuery] = useState(initialQuery);
  const [showRecent, setShowRecent] = useState(false);
  const [recentSearches, setRecentSearches] = useState<string[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Load recent searches on mount
  useEffect(() => {
    setRecentSearches(getRecentSearches());
  }, []);

  // Close dropdown on outside click
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setShowRecent(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const submitSearch = useCallback(
    (q: string) => {
      const trimmed = q.trim();
      if (!trimmed) return;
      saveRecentSearch(trimmed);
      setRecentSearches(getRecentSearches());
      setShowRecent(false);
      onSearch(trimmed);
    },
    [onSearch]
  );

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value;
    setQuery(value);

    // Debounced search
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (value.trim()) {
      debounceRef.current = setTimeout(() => {
        submitSearch(value);
      }, 500);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      if (debounceRef.current) clearTimeout(debounceRef.current);
      submitSearch(query);
    }
    if (e.key === 'Escape') {
      setShowRecent(false);
    }
  };

  const handleClear = () => {
    setQuery('');
    if (debounceRef.current) clearTimeout(debounceRef.current);
    inputRef.current?.focus();
  };

  const handleRecentClick = (term: string) => {
    setQuery(term);
    submitSearch(term);
  };

  const handleClearRecent = () => {
    localStorage.removeItem(RECENT_SEARCHES_KEY);
    setRecentSearches([]);
  };

  return (
    <div className="relative" ref={dropdownRef}>
      <label htmlFor="search-input" className="sr-only">
        Search edits
      </label>
      <div className="relative flex items-center">
        {/* Search icon */}
        <Search
          className="absolute left-4 h-5 w-5 text-gray-400 pointer-events-none"
          aria-hidden="true"
        />

        {/* Input */}
        <input
          ref={inputRef}
          id="search-input"
          type="text"
          value={query}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          onFocus={() => recentSearches.length > 0 && setShowRecent(true)}
          placeholder="Search pages, users, or comments..."
          aria-label="Search pages, users, or comments"
          className="w-full pl-12 pr-24 py-3 text-base border-2 border-gray-200 rounded-xl
            bg-white shadow-sm
            focus:ring-2 focus:ring-primary-500 focus:border-primary-500
            placeholder:text-gray-400 transition-all duration-200"
        />

        {/* Character count */}
        {query.length > 0 && (
          <span className="absolute right-28 text-xs text-gray-400 select-none" aria-hidden="true">
            {query.length}
          </span>
        )}

        {/* Clear button */}
        {query.length > 0 && (
          <button
            type="button"
            onClick={handleClear}
            className="absolute right-20 p-1 rounded-full text-gray-400 hover:text-gray-600 hover:bg-gray-100 transition-colors"
            aria-label="Clear search"
          >
            <X className="h-4 w-4" />
          </button>
        )}

        {/* Search button */}
        <button
          type="button"
          onClick={() => {
            if (debounceRef.current) clearTimeout(debounceRef.current);
            submitSearch(query);
          }}
          disabled={loading || !query.trim()}
          className="absolute right-2 px-4 py-1.5 bg-primary-600 text-white rounded-lg text-sm font-medium
            hover:bg-primary-700 disabled:opacity-50 disabled:cursor-not-allowed
            flex items-center gap-1.5 transition-colors"
          aria-label="Search"
        >
          {loading ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <>
              <Search className="h-3.5 w-3.5" />
              Search
            </>
          )}
        </button>
      </div>

      {/* Recent searches dropdown */}
      {showRecent && recentSearches.length > 0 && (
        <div
          className="absolute z-20 top-full left-0 right-0 mt-1 bg-white border border-gray-200 rounded-xl shadow-lg overflow-hidden"
          role="listbox"
          aria-label="Recent searches"
        >
          <div className="flex items-center justify-between px-4 py-2 border-b border-gray-100">
            <span className="text-xs font-medium text-gray-500 uppercase tracking-wide">
              Recent Searches
            </span>
            <button
              onClick={handleClearRecent}
              className="text-xs text-gray-400 hover:text-red-500 transition-colors"
            >
              Clear all
            </button>
          </div>
          {recentSearches.map((term) => (
            <button
              key={term}
              onClick={() => handleRecentClick(term)}
              className="w-full text-left px-4 py-2 text-sm text-gray-700 hover:bg-primary-50 hover:text-primary-700 flex items-center gap-2 transition-colors"
              role="option"
              aria-selected={false}
            >
              <Search className="h-3.5 w-3.5 text-gray-400 flex-shrink-0" />
              {term}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
