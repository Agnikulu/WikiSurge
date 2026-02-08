import { useState, useEffect, useRef, useCallback } from 'react';
import { Search, Loader2 } from 'lucide-react';
import { searchEdits } from '../../utils/api';
import type { Edit } from '../../types';

interface SearchSuggestionsProps {
  query: string;
  onSelect: (title: string) => void;
  visible: boolean;
  onClose: () => void;
}

export function SearchSuggestions({ query, onSelect, visible, onClose }: SearchSuggestionsProps) {
  const [suggestions, setSuggestions] = useState<Edit[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const fetchSuggestions = useCallback(async (q: string) => {
    if (!q.trim() || q.trim().length < 2) {
      setSuggestions([]);
      return;
    }
    setLoading(true);
    try {
      const data = await searchEdits(q.trim(), 5);
      setSuggestions(data.edits || []);
    } catch {
      setSuggestions([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!visible || !query.trim()) {
      setSuggestions([]);
      return;
    }
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => fetchSuggestions(query), 300);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [query, visible, fetchSuggestions]);

  // Reset selection when suggestions change
  useEffect(() => {
    setSelectedIndex(-1);
  }, [suggestions]);

  // Close on outside click
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onClose();
      }
    }
    if (visible) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [visible, onClose]);

  // Keyboard navigation
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (!visible || suggestions.length === 0) return;
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIndex((prev) => Math.min(prev + 1, suggestions.length - 1));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIndex((prev) => Math.max(prev - 1, 0));
      } else if (e.key === 'Enter' && selectedIndex >= 0) {
        e.preventDefault();
        onSelect(suggestions[selectedIndex].title);
      } else if (e.key === 'Escape') {
        onClose();
      }
    },
    [visible, suggestions, selectedIndex, onSelect, onClose]
  );

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  if (!visible || (!loading && suggestions.length === 0)) return null;

  return (
    <div
      ref={containerRef}
      className="absolute z-30 top-full left-0 right-0 mt-1 bg-white border border-gray-200 rounded-xl shadow-lg overflow-hidden"
      role="listbox"
      aria-label="Search suggestions"
    >
      {loading && (
        <div className="flex items-center gap-2 px-4 py-3 text-sm text-gray-500">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          Loading suggestions...
        </div>
      )}
      {!loading &&
        suggestions.map((edit, idx) => (
          <button
            key={`${edit.id}-${idx}`}
            onClick={() => onSelect(edit.title)}
            role="option"
            aria-selected={idx === selectedIndex}
            className={`w-full text-left px-4 py-2.5 text-sm flex items-center gap-2 transition-colors ${
              idx === selectedIndex
                ? 'bg-primary-50 text-primary-700'
                : 'text-gray-700 hover:bg-gray-50'
            }`}
          >
            <Search className="h-3.5 w-3.5 text-gray-400 flex-shrink-0" />
            <div className="flex-1 min-w-0">
              <p className="font-medium truncate">{edit.title}</p>
              <p className="text-xs text-gray-400 truncate">
                by {edit.user} · {edit.wiki}
              </p>
            </div>
          </button>
        ))}
    </div>
  );
}
