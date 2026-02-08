import { memo } from 'react';
import type { Edit } from '../../types';
import { highlightMatches } from '../../utils/highlight';
import {
  formatRelativeTime,
  formatByteChange,
  getByteChangeColor,
  getByteChange,
  extractLanguage,
  buildWikiUrl,
} from '../../utils/formatting';
import { Bot, User, ExternalLink } from 'lucide-react';

interface SearchResultItemProps {
  edit: Edit;
  query: string;
}

/**
 * A single search result row with query-term highlighting.
 */
export const SearchResultItem = memo(function SearchResultItem({
  edit,
  query,
}: SearchResultItemProps) {
  const byteChange = getByteChange(edit);
  const lang = extractLanguage(edit.wiki);
  const wikiUrl = buildWikiUrl(edit.title, edit.server_url);

  const renderHighlighted = (text: string) => {
    const segments = highlightMatches(text, query);
    return segments.map((seg, i) =>
      seg.match ? (
        <mark key={i} className="bg-yellow-200 text-yellow-900 font-semibold rounded-sm px-0.5">
          {seg.text}
        </mark>
      ) : (
        <span key={i}>{seg.text}</span>
      )
    );
  };

  return (
    <article className="flex items-start gap-3 p-4 rounded-lg border border-gray-100 hover:border-primary-200 hover:bg-primary-50/30 transition-all duration-200 group">
      {/* User / Bot indicator */}
      <div className="flex-shrink-0 mt-0.5" aria-hidden="true">
        {edit.bot ? (
          <div className="w-8 h-8 rounded-full bg-gray-100 flex items-center justify-center">
            <Bot className="h-4 w-4 text-gray-400" />
          </div>
        ) : (
          <div className="w-8 h-8 rounded-full bg-blue-50 flex items-center justify-center">
            <User className="h-4 w-4 text-blue-500" />
          </div>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0">
        {/* Title */}
        <div className="flex items-center gap-2 flex-wrap">
          <a
            href={wikiUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="font-semibold text-sm text-gray-900 hover:text-primary-700 transition-colors inline-flex items-center gap-1"
            title={edit.title}
          >
            <span className="truncate max-w-[400px]">{renderHighlighted(edit.title)}</span>
            <ExternalLink className="h-3 w-3 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0" />
          </a>
          {edit.bot && (
            <span className="text-[10px] leading-none px-1.5 py-0.5 rounded-full bg-gray-100 text-gray-500 font-medium">
              bot
            </span>
          )}
        </div>

        {/* Meta */}
        <div className="flex items-center gap-1.5 mt-1 text-xs text-gray-500 flex-wrap">
          <span className="text-gray-600">
            Edited by <span className="font-medium">{renderHighlighted(edit.user)}</span>
          </span>
          <span aria-hidden="true">·</span>
          <span className="inline-flex items-center px-1.5 py-0 rounded bg-gray-100 text-gray-500 text-[10px] font-medium uppercase tracking-wide">
            {lang}
          </span>
          <span aria-hidden="true">·</span>
          <time
            dateTime={
              typeof edit.timestamp === 'number'
                ? new Date(edit.timestamp * 1000).toISOString()
                : edit.timestamp
            }
            className="text-gray-400"
          >
            {formatRelativeTime(edit.timestamp)}
          </time>
        </div>

        {/* Comment */}
        {edit.comment && (
          <p className="mt-1.5 text-xs text-gray-500 line-clamp-2" title={edit.comment}>
            {renderHighlighted(edit.comment)}
          </p>
        )}
      </div>

      {/* Byte change */}
      <div className="flex-shrink-0 text-right">
        <span
          className={`text-sm font-mono font-semibold ${getByteChangeColor(byteChange)}`}
          aria-label={`${byteChange >= 0 ? 'Added' : 'Removed'} ${Math.abs(byteChange)} bytes`}
        >
          {formatByteChange(byteChange)}
        </span>
      </div>
    </article>
  );
});
