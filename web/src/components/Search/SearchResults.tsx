import type { Edit } from '../../types';
import { EditItem } from '../LiveFeed/EditItem';

interface SearchResultsProps {
  results: Edit[];
  loading: boolean;
}

export function SearchResults({ results, loading }: SearchResultsProps) {
  if (loading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="animate-pulse">
            <div className="h-4 bg-gray-200 rounded w-3/4 mb-1" />
            <div className="h-3 bg-gray-200 rounded w-1/2" />
          </div>
        ))}
      </div>
    );
  }

  if (results.length === 0) {
    return (
      <p className="text-sm text-gray-400 text-center py-6">
        No results found. Try a different search term.
      </p>
    );
  }

  return (
    <div className="space-y-1 max-h-96 overflow-y-auto">
      <p className="text-xs text-gray-500 mb-2">{results.length} results</p>
      {results.map((edit, index) => (
        <EditItem key={`${edit.id}-${index}`} edit={edit} />
      ))}
    </div>
  );
}
