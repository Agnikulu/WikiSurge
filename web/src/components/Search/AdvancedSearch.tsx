import { useState } from 'react';
import type { SearchParams } from '../../types';
import { X, SlidersHorizontal, HelpCircle } from 'lucide-react';

interface AdvancedSearchProps {
  isOpen: boolean;
  onClose: () => void;
  onApply: (filters: Partial<SearchParams>) => void;
  currentFilters: Partial<SearchParams>;
}

const LANGUAGES = [
  'en', 'es', 'de', 'fr', 'ja', 'ru', 'zh', 'pt', 'it', 'ar',
  'nl', 'pl', 'sv', 'uk', 'vi', 'ko', 'fi', 'fa', 'he', 'cs',
];

export function AdvancedSearch({ isOpen, onClose, onApply, currentFilters }: AdvancedSearchProps) {
  const [dateFrom, setDateFrom] = useState(currentFilters.from || '');
  const [dateTo, setDateTo] = useState(currentFilters.to || '');
  const [language, setLanguage] = useState(currentFilters.language || '');
  const [user, setUser] = useState(currentFilters.user || '');
  const [excludeBots, setExcludeBots] = useState(currentFilters.exclude_bots || false);
  const [minBytes, setMinBytes] = useState(
    currentFilters.min_bytes !== undefined ? String(currentFilters.min_bytes) : ''
  );
  const [maxBytes, setMaxBytes] = useState(
    currentFilters.max_bytes !== undefined ? String(currentFilters.max_bytes) : ''
  );
  const [sort, setSort] = useState<SearchParams['sort']>(currentFilters.sort || 'relevance');
  const [showHelp, setShowHelp] = useState(false);

  if (!isOpen) return null;

  const handleApply = () => {
    const filters: Partial<SearchParams> = {};
    if (dateFrom) filters.from = dateFrom;
    if (dateTo) filters.to = dateTo;
    if (language) filters.language = language;
    if (user.trim()) filters.user = user.trim();
    if (excludeBots) filters.exclude_bots = true;
    if (minBytes) filters.min_bytes = Number(minBytes);
    if (maxBytes) filters.max_bytes = Number(maxBytes);
    if (sort) filters.sort = sort;
    onApply(filters);
    onClose();
  };

  const handleReset = () => {
    setDateFrom('');
    setDateTo('');
    setLanguage('');
    setUser('');
    setExcludeBots(false);
    setMinBytes('');
    setMaxBytes('');
    setSort('relevance');
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-label="Advanced search">
      {/* Backdrop */}
      <div className="absolute inset-0 backdrop-blur-sm" style={{ background: 'rgba(0,0,0,0.6)' }} onClick={onClose} />

      {/* Modal */}
      <div className="relative w-full max-w-lg rounded-2xl shadow-xl animate-slide-up" style={{ background: '#111b2e', border: '1px solid rgba(0,255,136,0.15)' }}>
        {/* Header */}
        <div className="flex items-center justify-between p-5" style={{ borderBottom: '1px solid rgba(0,255,136,0.1)' }}>
          <div className="flex items-center gap-2">
            <SlidersHorizontal className="h-5 w-5" style={{ color: '#00ff88' }} />
            <h2 style={{ color: '#00ff88', fontFamily: 'monospace', fontSize: '0.85rem', fontWeight: 700, letterSpacing: '0.1em' }}>ADVANCED SEARCH</h2>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-lg transition-colors"
            style={{ color: 'rgba(0,255,136,0.4)' }}
            aria-label="Close advanced search"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Body */}
        <div className="p-5 space-y-4 max-h-[60vh] overflow-y-auto">
          {/* Date Range */}
          <fieldset>
            <legend className="text-sm font-medium mb-1.5" style={{ color: 'rgba(0,255,136,0.6)', fontFamily: 'monospace' }}>DATE RANGE</legend>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label htmlFor="date-from" className="text-xs" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
                  FROM
                </label>
                <input
                  id="date-from"
                  type="date"
                  value={dateFrom}
                  onChange={(e) => setDateFrom(e.target.value)}
                  className="w-full mt-0.5 px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-1"
                  style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }}
                />
              </div>
              <div>
                <label htmlFor="date-to" className="text-xs" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
                  TO
                </label>
                <input
                  id="date-to"
                  type="date"
                  value={dateTo}
                  onChange={(e) => setDateTo(e.target.value)}
                  className="w-full mt-0.5 px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-1"
                  style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }}
                />
              </div>
            </div>
          </fieldset>

          {/* Language */}
          <div>
            <label htmlFor="language" className="block text-sm font-medium mb-1.5" style={{ color: 'rgba(0,255,136,0.6)', fontFamily: 'monospace' }}>
              LANGUAGE
            </label>
            <select
              id="language"
              value={language}
              onChange={(e) => setLanguage(e.target.value)}
              className="w-full px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-1"
              style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }}
            >
              <option value="">All languages</option>
              {LANGUAGES.map((l) => (
                <option key={l} value={l}>
                  {l}wiki
                </option>
              ))}
            </select>
          </div>

          {/* User */}
          <div>
            <label htmlFor="user-filter" className="block text-sm font-medium mb-1.5" style={{ color: 'rgba(0,255,136,0.6)', fontFamily: 'monospace' }}>
              USER NAME
            </label>
            <input
              id="user-filter"
              type="text"
              value={user}
              onChange={(e) => setUser(e.target.value)}
              placeholder="FILTER BY USER NAME..."
              className="w-full px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-1"
              style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }}
            />
          </div>

          {/* Byte Change Range */}
          <fieldset>
            <legend className="text-sm font-medium mb-1.5" style={{ color: 'rgba(0,255,136,0.6)', fontFamily: 'monospace' }}>
              BYTE CHANGE RANGE
            </legend>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label htmlFor="min-bytes" className="text-xs" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
                  MIN
                </label>
                <input
                  id="min-bytes"
                  type="number"
                  value={minBytes}
                  onChange={(e) => setMinBytes(e.target.value)}
                  placeholder="0"
                  className="w-full mt-0.5 px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-1"
                  style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }}
                />
              </div>
              <div>
                <label htmlFor="max-bytes" className="text-xs" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
                  MAX
                </label>
                <input
                  id="max-bytes"
                  type="number"
                  value={maxBytes}
                  onChange={(e) => setMaxBytes(e.target.value)}
                  placeholder="No limit"
                  className="w-full mt-0.5 px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-1"
                  style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }}
                />
              </div>
            </div>
          </fieldset>

          {/* Exclude Bots */}
          <div className="flex items-center gap-2">
            <input
              id="exclude-bots"
              type="checkbox"
              checked={excludeBots}
              onChange={(e) => setExcludeBots(e.target.checked)}
              className="h-4 w-4 rounded"
              style={{ accentColor: '#00ff88' }}
            />
            <label htmlFor="exclude-bots" className="text-sm" style={{ color: 'rgba(0,255,136,0.6)', fontFamily: 'monospace' }}>
              EXCLUDE BOT EDITS
            </label>
          </div>

          {/* Sort */}
          <div>
            <label htmlFor="sort-by" className="block text-sm font-medium mb-1.5" style={{ color: 'rgba(0,255,136,0.6)', fontFamily: 'monospace' }}>
              SORT BY
            </label>
            <select
              id="sort-by"
              value={sort}
              onChange={(e) => setSort(e.target.value as SearchParams['sort'])}
              className="w-full px-3 py-2 rounded-lg text-sm focus:outline-none focus:ring-1"
              style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.15)', color: '#00ff88', fontFamily: 'monospace' }}
            >
              <option value="relevance">Relevance</option>
              <option value="date_desc">Date (newest first)</option>
              <option value="date_asc">Date (oldest first)</option>
            </select>
          </div>

          {/* Syntax Help */}
          <button
            type="button"
            onClick={() => setShowHelp(!showHelp)}
            className="flex items-center gap-1 text-xs transition-colors"
            style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}
          >
            <HelpCircle className="h-3.5 w-3.5" />
            SYNTAX HELP
          </button>
          {showHelp && (
            <div className="p-3 rounded-lg text-xs space-y-1" style={{ background: 'rgba(0,221,255,0.05)', border: '1px solid rgba(0,221,255,0.1)', color: '#00ddff', fontFamily: 'monospace' }}>
              <p><strong>title:</strong>keyword — search in page titles only</p>
              <p><strong>user:</strong>name — filter by user name</p>
              <p><strong>&quot;exact phrase&quot;</strong> — match exact phrase</p>
              <p><strong>-term</strong> — exclude results containing term</p>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between p-5" style={{ borderTop: '1px solid rgba(0,255,136,0.1)' }}>
          <button
            onClick={handleReset}
            className="px-4 py-2 text-sm transition-colors"
            style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}
          >
            RESET
          </button>
          <div className="flex gap-2">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm rounded-lg transition-colors"
              style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}
            >
              CANCEL
            </button>
            <button
              onClick={handleApply}
              className="px-5 py-2 rounded-lg text-sm font-medium transition-colors"
              style={{ background: 'rgba(0,255,136,0.15)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.3)', fontFamily: 'monospace' }}
            >
              APPLY FILTERS
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
