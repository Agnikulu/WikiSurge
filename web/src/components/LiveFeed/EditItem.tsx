import { memo } from 'react';
import type { Edit } from '../../types';
import {
  formatRelativeTime,
  formatByteChange,
  getByteChangeColor,
  truncateTitle,
  getByteChange,
  extractLanguage,
  isNewPage,
} from '../../utils/formatting';
import { Bot, User, FilePlus, Zap } from 'lucide-react';

interface EditItemProps {
  edit: Edit;
  onClick?: (edit: Edit) => void;
}

export const EditItem = memo(function EditItem({ edit, onClick }: EditItemProps) {
  const byteChange = getByteChange(edit);
  const absChange = Math.abs(byteChange);
  const isLargeEdit = absChange > 1000;
  const newPage = isNewPage(edit);
  const lang = extractLanguage(edit.wiki);

  const handleClick = () => {
    onClick?.(edit);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onClick?.(edit);
    }
  };

  return (
    <article
      role="button"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      aria-label={`Edit to ${edit.title} by ${edit.user}, ${formatByteChange(byteChange)}`}
      className={`
        edit-item flex items-start gap-3 p-3 rounded-lg cursor-pointer
        transition-all duration-200 border
      `}
      style={{
        borderColor: isLargeEdit ? 'rgba(255,170,0,0.3)' : newPage ? 'rgba(0,255,136,0.3)' : 'transparent',
        borderLeftWidth: isLargeEdit || newPage ? '2px' : undefined,
        background: edit.bot ? 'rgba(0,255,136,0.02)' : 'transparent',
      }}
    >
      {/* User / Bot indicator */}
      <div className="flex-shrink-0 mt-0.5" aria-hidden="true">
        {edit.bot ? (
          <div className="w-7 h-7 rounded-full flex items-center justify-center" style={{ background: 'rgba(0,255,136,0.06)' }}>
            <Bot className="h-3.5 w-3.5" style={{ color: 'rgba(0,255,136,0.3)' }} aria-label="Bot edit" />
          </div>
        ) : (
          <div className="w-7 h-7 rounded-full flex items-center justify-center" style={{ background: 'rgba(0,255,136,0.1)' }}>
            <User className="h-3.5 w-3.5" style={{ color: '#00ff88' }} aria-label="User edit" />
          </div>
        )}
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0">
        {/* Title row */}
        <div className="flex items-center gap-1.5 flex-wrap">
          <h3
            className="font-semibold text-sm truncate max-w-[240px]"
            style={{ color: edit.bot ? 'rgba(0,255,136,0.5)' : '#00ff88', fontFamily: 'monospace' }}
            title={edit.title}
          >
            {truncateTitle(edit.title, 45)}
          </h3>
          {edit.bot && (
            <span className="badge badge-bot text-[10px] leading-none px-1.5 py-0.5">bot</span>
          )}
          {newPage && (
            <span
              className="inline-flex items-center gap-0.5 text-[10px] leading-none px-1.5 py-0.5 rounded-full font-medium"
              style={{ background: 'rgba(0,255,136,0.15)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.3)' }}
              aria-label="New page"
            >
              <FilePlus className="h-2.5 w-2.5" />
              new
            </span>
          )}
          {isLargeEdit && (
            <span
              className="inline-flex items-center gap-0.5 text-[10px] leading-none px-1.5 py-0.5 rounded-full font-medium"
              style={{ background: 'rgba(255,170,0,0.15)', color: '#ffaa00', border: '1px solid rgba(255,170,0,0.3)' }}
              aria-label="Large edit"
            >
              <Zap className="h-2.5 w-2.5" />
              large
            </span>
          )}
        </div>

        {/* Meta row */}
        <div className="flex items-center gap-1.5 mt-1 text-xs flex-wrap" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
          <span style={{ color: edit.bot ? 'rgba(0,255,136,0.3)' : 'rgba(0,255,136,0.6)' }}>{edit.user}</span>
          <span aria-hidden="true">·</span>
          <span
            className="inline-flex items-center px-1.5 py-0 rounded text-[10px] font-medium uppercase tracking-wide"
            style={{ background: 'rgba(0,221,255,0.1)', color: '#00ddff' }}
            aria-label={`Language: ${lang}`}
          >
            {lang}
          </span>
          <span aria-hidden="true">·</span>
          <time
            dateTime={
              typeof edit.timestamp === 'number'
                ? new Date(edit.timestamp * 1000).toISOString()
                : edit.timestamp
            }
            style={{ color: 'rgba(0,255,136,0.3)' }}
          >
            {formatRelativeTime(edit.timestamp)}
          </time>
        </div>

        {/* Comment */}
        {edit.comment && (
          <p
            className="mt-1 text-xs truncate max-w-[320px]"
            style={{ color: 'rgba(0,255,136,0.25)', fontFamily: 'monospace' }}
            title={edit.comment}
          >
            {truncateTitle(edit.comment, 80)}
          </p>
        )}
      </div>

      {/* Byte change */}
      <div className="flex-shrink-0 text-right">
        <span
          className={`text-sm font-mono font-semibold ${getByteChangeColor(byteChange)}`}
          aria-label={`${byteChange >= 0 ? 'Added' : 'Removed'} ${absChange} bytes`}
        >
          {formatByteChange(byteChange)}
        </span>
      </div>
    </article>
  );
});
