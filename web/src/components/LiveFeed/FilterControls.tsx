import { useCallback } from 'react';
import { Filter, X } from 'lucide-react';
import { useAppStore } from '../../store/appStore';

const COMMON_LANGUAGES = [
  { code: '', label: 'All languages' },
  { code: 'en', label: 'English' },
  { code: 'es', label: 'Español' },
  { code: 'fr', label: 'Français' },
  { code: 'de', label: 'Deutsch' },
  { code: 'ja', label: '日本語' },
  { code: 'zh', label: '中文' },
  { code: 'ru', label: 'Русский' },
  { code: 'pt', label: 'Português' },
  { code: 'it', label: 'Italiano' },
  { code: 'ar', label: 'العربية' },
] as const;

interface FilterControlsProps {
  onFilterChange?: () => void;
}

export function FilterControls({ onFilterChange }: FilterControlsProps) {
  const filters = useAppStore((s) => s.filters);
  const setFilters = useAppStore((s) => s.setFilters);
  const resetFilters = useAppStore((s) => s.resetFilters);

  const hasActiveFilters =
    filters.languages.length > 0 || filters.excludeBots || filters.minByteChange > 0;

  const handleLanguageToggle = useCallback(
    (code: string) => {
      if (!code) {
        setFilters({ languages: [] });
      } else {
        const current = filters.languages;
        const updated = current.includes(code)
          ? current.filter((l) => l !== code)
          : [...current, code];
        setFilters({ languages: updated });
      }
      onFilterChange?.();
    },
    [filters.languages, setFilters, onFilterChange]
  );

  const handleBotsToggle = useCallback(() => {
    setFilters({ excludeBots: !filters.excludeBots });
    onFilterChange?.();
  }, [filters.excludeBots, setFilters, onFilterChange]);

  const handleMinByteChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      setFilters({ minByteChange: Number(e.target.value) });
      onFilterChange?.();
    },
    [setFilters, onFilterChange]
  );

  const handleReset = useCallback(() => {
    resetFilters();
    onFilterChange?.();
  }, [resetFilters, onFilterChange]);

  return (
    <div className="space-y-3">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-xs font-medium text-gray-500 uppercase tracking-wider">
          <Filter className="h-3 w-3" />
          Filters
        </div>
        {hasActiveFilters && (
          <button
            onClick={handleReset}
            className="flex items-center gap-1 text-xs text-gray-400 hover:text-gray-600 transition-colors"
            aria-label="Clear all filters"
          >
            <X className="h-3 w-3" />
            Clear
          </button>
        )}
      </div>

      {/* Language selector */}
      <div>
        <label className="block text-xs font-medium text-gray-600 mb-1.5">Languages</label>
        <div className="flex flex-wrap gap-1">
          {COMMON_LANGUAGES.map(({ code, label }) => {
            const isActive = code === '' ? filters.languages.length === 0 : filters.languages.includes(code);
            return (
              <button
                key={code || 'all'}
                onClick={() => handleLanguageToggle(code)}
                className={`
                  px-2 py-0.5 rounded-full text-[11px] font-medium transition-all duration-150
                  ${
                    isActive
                      ? 'bg-blue-100 text-blue-700 ring-1 ring-blue-200'
                      : 'bg-gray-100 text-gray-500 hover:bg-gray-200'
                  }
                `}
                aria-pressed={isActive}
                aria-label={`Filter by ${label}`}
              >
                {code || 'All'}
              </button>
            );
          })}
        </div>
      </div>

      {/* Bot toggle */}
      <div className="flex items-center justify-between">
        <label htmlFor="exclude-bots" className="text-xs font-medium text-gray-600">
          Exclude bots
        </label>
        <button
          id="exclude-bots"
          role="switch"
          aria-checked={filters.excludeBots}
          onClick={handleBotsToggle}
          className={`
            relative inline-flex h-5 w-9 items-center rounded-full transition-colors duration-200
            ${filters.excludeBots ? 'bg-blue-500' : 'bg-gray-200'}
          `}
        >
          <span
            className={`
              inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform duration-200
              ${filters.excludeBots ? 'translate-x-4' : 'translate-x-0.5'}
            `}
          />
        </button>
      </div>

      {/* Min byte change slider */}
      <div>
        <div className="flex items-center justify-between mb-1">
          <label htmlFor="min-byte-change" className="text-xs font-medium text-gray-600">
            Min byte change
          </label>
          <span className="text-[11px] text-gray-400 font-mono tabular-nums">
            {filters.minByteChange > 0 ? `±${filters.minByteChange}` : 'any'}
          </span>
        </div>
        <input
          id="min-byte-change"
          type="range"
          min={0}
          max={1000}
          step={10}
          value={filters.minByteChange}
          onChange={handleMinByteChange}
          className="w-full h-1.5 bg-gray-200 rounded-full appearance-none cursor-pointer accent-blue-500
            [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:w-3.5 [&::-webkit-slider-thumb]:h-3.5
            [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-blue-500 [&::-webkit-slider-thumb]:cursor-pointer
            [&::-webkit-slider-thumb]:shadow-sm"
          aria-label={`Minimum byte change: ${filters.minByteChange}`}
        />
        <div className="flex justify-between text-[10px] text-gray-300 mt-0.5">
          <span>0</span>
          <span>500</span>
          <span>1000</span>
        </div>
      </div>
    </div>
  );
}
