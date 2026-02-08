import type { TrendingPage } from '../../types';
import { formatTimestamp, formatNumber, truncateTitle } from '../../utils/formatting';
import { TrendingUp } from 'lucide-react';

interface TrendingCardProps {
  page: TrendingPage;
}

export function TrendingCard({ page }: TrendingCardProps) {
  return (
    <div className="flex items-center space-x-3 p-2 rounded-lg hover:bg-gray-50 transition-colors">
      {/* Rank */}
      <span className="text-lg font-bold text-gray-300 w-8 text-center">
        {page.rank}
      </span>

      {/* Info */}
      <div className="flex-1 min-w-0">
        <p className="font-medium text-gray-900 truncate" title={page.title}>
          {truncateTitle(page.title, 45)}
        </p>
        <div className="flex items-center space-x-2 text-xs text-gray-500">
          <span>{page.language}</span>
          <span>·</span>
          <span>{page.edits_1h} edits/hr</span>
          <span>·</span>
          <span>{formatTimestamp(page.last_edit)}</span>
        </div>
      </div>

      {/* Score */}
      <div className="flex items-center space-x-1 text-sm">
        <TrendingUp className="h-3.5 w-3.5 text-green-500" />
        <span className="font-semibold text-gray-700">{formatNumber(Math.round(page.score))}</span>
      </div>
    </div>
  );
}
