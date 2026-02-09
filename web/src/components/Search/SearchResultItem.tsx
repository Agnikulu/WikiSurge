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
        <mark key={i} className="font-semibold rounded-sm px-0.5" style={{ background: 'rgba(0,255,136,0.2)', color: '#00ff88' }}>
          {seg.text}
        </mark>
      ) : (
        <span key={i}>{seg.text}</span>
      )
    );
  };

  return (
    <article className="flex items-start gap-3 p-4 rounded-lg transition-all duration-200 group" style={{ border: '1px solid rgba(0,255,136,0.08)' }}>
      {/* User / Bot indicator */}
      <div className="flex-shrink-0 mt-0.5" aria-hidden="true">
        {edit.bot ? (
          <div className="w-8 h-8 rounded-full flex items-center justify-center" style={{ background: 'rgba(0,255,136,0.05)' }}>
            <Bot className="h-4 w-4" style={{ color: 'rgba(0,255,136,0.3)' }} />
          </div>
        ) : (
          <div className="w-8 h-8 rounded-full flex items-center justify-center" style={{ background: 'rgba(0,255,136,0.1)' }}>
            <User className="h-4 w-4" style={{ color: '#00ff88' }} />
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
            className="font-semibold text-sm transition-colors inline-flex items-center gap-1"
            style={{ color: '#00ff88', fontFamily: 'monospace' }}
            title={edit.title}
          >
            <span className="truncate max-w-[400px]">{renderHighlighted(edit.title)}</span>
            <ExternalLink className="h-3 w-3 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0" />
          </a>
          {edit.bot && (
            <span className="text-[10px] leading-none px-1.5 py-0.5 rounded-full font-medium" style={{ background: 'rgba(0,255,136,0.08)', color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
              BOT
            </span>
          )}
        </div>

        {/* Meta */}
        <div className="flex items-center gap-1.5 mt-1 text-xs flex-wrap" style={{ color: 'rgba(0,255,136,0.4)' }}>
          <span style={{ color: 'rgba(0,255,136,0.5)' }}>
            Edited by <span className="font-medium" style={{ color: '#00ff88' }}>{renderHighlighted(edit.user)}</span>
          </span>
          <span aria-hidden="true">·</span>
          <span className="inline-flex items-center px-1.5 py-0 rounded text-[10px] font-medium uppercase tracking-wide" style={{ background: 'rgba(0,221,255,0.1)', color: '#00ddff', fontFamily: 'monospace' }}>
            {lang}
          </span>
          <span aria-hidden="true">·</span>
          <time
            dateTime={
              typeof edit.timestamp === 'number'
                ? new Date(edit.timestamp * 1000).toISOString()
                : edit.timestamp
            }
            style={{ color: 'rgba(0,255,136,0.3)', fontFamily: 'monospace' }}
          >
            {formatRelativeTime(edit.timestamp)}
          </time>
        </div>

        {/* Comment */}
        {edit.comment && (
          <p className="mt-1.5 text-xs line-clamp-2" style={{ color: 'rgba(0,255,136,0.3)', fontFamily: 'monospace' }} title={edit.comment}>
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
