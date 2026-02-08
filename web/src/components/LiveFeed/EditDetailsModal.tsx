import { useEffect, useRef, useCallback } from 'react';
import { createPortal } from 'react-dom';
import type { Edit } from '../../types';
import {
  formatByteChange,
  getByteChange,
  getByteChangeColor,
  extractLanguage,
  isNewPage,
  buildWikiUrl,
  buildDiffUrl,
  buildUserUrl,
} from '../../utils/formatting';
import {
  X,
  ExternalLink,
  Bot,
  User,
  FilePlus,
  Clock,
  MessageSquare,
  ArrowRightLeft,
  Globe,
} from 'lucide-react';

interface EditDetailsModalProps {
  edit: Edit;
  onClose: () => void;
}

export function EditDetailsModal({ edit, onClose }: EditDetailsModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<Element | null>(null);

  const byteChange = getByteChange(edit);
  const lang = extractLanguage(edit.wiki);
  const newPage = isNewPage(edit);
  const serverUrl = edit.server_url || `https://${lang}.wikipedia.org`;

  // Focus trap + ESC handling
  useEffect(() => {
    previousFocusRef.current = document.activeElement;
    contentRef.current?.focus();

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
        return;
      }

      // Focus trap
      if (e.key === 'Tab' && contentRef.current) {
        const focusable = contentRef.current.querySelectorAll<HTMLElement>(
          'a, button, [tabindex]:not([tabindex="-1"])'
        );
        if (focusable.length === 0) return;

        const first = focusable[0];
        const last = focusable[focusable.length - 1];

        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault();
            last.focus();
          }
        } else {
          if (document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    document.body.style.overflow = 'hidden';

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
      if (previousFocusRef.current instanceof HTMLElement) {
        previousFocusRef.current.focus();
      }
    };
  }, [onClose]);

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === overlayRef.current) {
        onClose();
      }
    },
    [onClose]
  );

  const fullDate =
    typeof edit.timestamp === 'number'
      ? new Date(edit.timestamp * 1000)
      : new Date(edit.timestamp);

  const modal = (
    <div
      ref={overlayRef}
      onClick={handleOverlayClick}
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/40 backdrop-blur-sm animate-fade-in"
      role="dialog"
      aria-modal="true"
      aria-label={`Edit details for ${edit.title}`}
    >
      <div
        ref={contentRef}
        tabIndex={-1}
        className="bg-white rounded-xl shadow-2xl w-full max-w-lg max-h-[85vh] overflow-y-auto animate-slide-up outline-none"
      >
        {/* Header */}
        <div className="sticky top-0 bg-white border-b border-gray-100 px-5 py-4 flex items-start justify-between rounded-t-xl">
          <div className="flex-1 min-w-0 pr-4">
            <h2 className="text-lg font-bold text-gray-900 leading-tight break-words">
              {edit.title}
            </h2>
            <div className="flex items-center gap-2 mt-1">
              <span className="inline-flex items-center px-2 py-0.5 rounded bg-gray-100 text-gray-500 text-[11px] font-medium uppercase tracking-wide">
                {lang}
              </span>
              {edit.bot && <span className="badge badge-bot text-[10px]">bot</span>}
              {newPage && (
                <span className="inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded-full bg-emerald-100 text-emerald-700 font-medium">
                  <FilePlus className="h-2.5 w-2.5" />
                  new page
                </span>
              )}
            </div>
          </div>
          <button
            onClick={onClose}
            className="flex-shrink-0 p-1.5 rounded-lg hover:bg-gray-100 transition-colors text-gray-400 hover:text-gray-600"
            aria-label="Close modal"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Body */}
        <div className="px-5 py-4 space-y-4">
          {/* User */}
          <DetailRow
            icon={edit.bot ? Bot : User}
            label="Editor"
            value={
              <a
                href={buildUserUrl(edit.user, serverUrl)}
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-600 hover:text-blue-700 hover:underline inline-flex items-center gap-1"
              >
                {edit.user}
                <ExternalLink className="h-3 w-3" />
              </a>
            }
          />

          {/* Byte change */}
          <DetailRow
            icon={ArrowRightLeft}
            label="Size change"
            value={
              <div>
                <span className={`font-mono font-bold text-base ${getByteChangeColor(byteChange)}`}>
                  {formatByteChange(byteChange)}
                </span>
                <span className="text-gray-400 ml-2 text-xs">
                  ({edit.length?.old?.toLocaleString() ?? '?'} → {edit.length?.new?.toLocaleString() ?? '?'} bytes)
                </span>
              </div>
            }
          />

          {/* Timestamp */}
          <DetailRow
            icon={Clock}
            label="Timestamp"
            value={
              <span>
                {fullDate.toLocaleString()} <span className="text-gray-400 text-xs">({fullDate.toISOString()})</span>
              </span>
            }
          />

          {/* Comment */}
          {edit.comment && (
            <DetailRow
              icon={MessageSquare}
              label="Comment"
              value={<span className="text-gray-700 break-words">{edit.comment}</span>}
            />
          )}

          {/* Wiki info */}
          <DetailRow
            icon={Globe}
            label="Wiki"
            value={<span>{edit.wiki}</span>}
          />

          {/* Revision IDs */}
          {edit.revision && (
            <DetailRow
              icon={ArrowRightLeft}
              label="Revisions"
              value={
                <span className="font-mono text-xs text-gray-500">
                  {edit.revision.old} → {edit.revision.new}
                </span>
              }
            />
          )}
        </div>

        {/* Footer links */}
        <div className="border-t border-gray-100 px-5 py-3 flex flex-wrap gap-2">
          <a
            href={buildWikiUrl(edit.title, serverUrl)}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-blue-50 text-blue-700 text-xs font-medium hover:bg-blue-100 transition-colors"
          >
            <ExternalLink className="h-3 w-3" />
            View page
          </a>
          {edit.revision && edit.revision.old > 0 && (
            <a
              href={buildDiffUrl(edit.revision.old, edit.revision.new, serverUrl)}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-gray-50 text-gray-700 text-xs font-medium hover:bg-gray-100 transition-colors"
            >
              <ArrowRightLeft className="h-3 w-3" />
              View diff
            </a>
          )}
          <a
            href={buildUserUrl(edit.user, serverUrl)}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-gray-50 text-gray-700 text-xs font-medium hover:bg-gray-100 transition-colors"
          >
            <User className="h-3 w-3" />
            User page
          </a>
        </div>
      </div>
    </div>
  );

  return createPortal(modal, document.body);
}

/** Reusable detail row with icon */
function DetailRow({
  icon: Icon,
  label,
  value,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: React.ReactNode;
}) {
  return (
    <div className="flex items-start gap-3">
      <div className="flex-shrink-0 w-8 h-8 rounded-lg bg-gray-50 flex items-center justify-center mt-0.5">
        <Icon className="h-4 w-4 text-gray-400" />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-[11px] font-medium text-gray-400 uppercase tracking-wider mb-0.5">
          {label}
        </p>
        <div className="text-sm text-gray-800">{value}</div>
      </div>
    </div>
  );
}
