import { useState } from 'react';
import type { EditWar } from '../../types';
import {
  buildWikiUrl,
  formatTimestamp,
  getSeverityColor,
  truncateTitle,
} from '../../utils/formatting';
import { SeverityBadge } from '../Alerts/SeverityBadge';
import { EditorConflictGraph } from './EditorConflictGraph';
import { EditWarTimeline } from './EditWarTimeline';
import {
  ChevronDown,
  ChevronUp,
  Clock,
  ExternalLink,
  History,
  RotateCcw,
  Swords,
  Users,
  X,
  FileEdit,
} from 'lucide-react';

interface EditWarCardProps {
  war: EditWar;
  onDismiss?: (war: EditWar) => void;
  /** If true the card was just inserted (triggers highlight animation) */
  isNew?: boolean;
}

/* ---------- Severity-specific border accent ---------- */
const SEVERITY_BORDER: Record<string, string> = {
  critical: 'border-l-red-500',
  high: 'border-l-orange-500',
  medium: 'border-l-yellow-500',
  low: 'border-l-blue-400',
};

/**
 * Calculate a human-readable duration from the start_time to now.
 */
function durationSince(isoTimestamp: string): string {
  try {
    const start = new Date(isoTimestamp).getTime();
    const diffMs = Date.now() - start;
    if (diffMs < 0) return 'just started';

    const mins = Math.floor(diffMs / 60_000);
    if (mins < 1) return 'less than a minute';
    if (mins < 60) return `${mins} minute${mins !== 1 ? 's' : ''}`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ${mins % 60}m`;
    const days = Math.floor(hours / 24);
    return `${days}d ${hours % 24}h`;
  } catch {
    return 'unknown';
  }
}

export function EditWarCard({ war, onDismiss, isNew = false }: EditWarCardProps) {
  const [expanded, setExpanded] = useState(false);
  const severity = getSeverityColor(war.severity);
  const accentBorder =
    SEVERITY_BORDER[war.severity.toLowerCase()] ?? 'border-l-blue-400';
  const wikiUrl = buildWikiUrl(war.page_title);
  const historyUrl = `${wikiUrl}?action=history`;

  return (
    <div
      className={`
        relative rounded-lg border border-l-4 ${severity.bg} ${severity.border} ${accentBorder}
        transition-all duration-300 group
        hover:shadow-md
        ${isNew ? 'animate-slide-up ring-2 ring-yellow-300' : ''}
      `}
    >
      {/* ── Main row ─────────────────────────────── */}
      <div className="p-4">
        <div className="flex items-start justify-between gap-2">
          {/* Left: icon + title + badges */}
          <div className="flex items-start gap-2 min-w-0">
            <Swords className={`h-5 w-5 mt-0.5 flex-shrink-0 ${severity.text}`} />
            <div className="min-w-0">
              {/* Title */}
              <a
                href={wikiUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sm font-bold text-gray-900 hover:text-blue-600 hover:underline leading-tight block truncate"
                title={war.page_title}
              >
                {truncateTitle(war.page_title, 55)}
              </a>

              {/* Badges */}
              <div className="flex flex-wrap items-center gap-1.5 mt-1">
                {/* Status badge */}
                <span
                  className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-semibold uppercase tracking-wide ${
                    war.active
                      ? 'bg-red-100 text-red-700'
                      : 'bg-gray-100 text-gray-600'
                  }`}
                >
                  <span
                    className={`inline-block w-1.5 h-1.5 rounded-full ${
                      war.active ? 'bg-red-500 animate-pulse' : 'bg-gray-400'
                    }`}
                  />
                  {war.active ? 'Active' : 'Resolved'}
                </span>

                <SeverityBadge severity={war.severity} />
              </div>
            </div>
          </div>

          {/* Right: dismiss */}
          {onDismiss && (
            <button
              onClick={() => onDismiss(war)}
              className="p-1 rounded text-gray-400 opacity-0 group-hover:opacity-100 hover:bg-white/70 transition-opacity flex-shrink-0"
              aria-label="Dismiss edit war"
            >
              <X className="h-4 w-4" />
            </button>
          )}
        </div>

        {/* ── Statistics grid ─────────────────────── */}
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mt-3">
          <StatCell
            icon={<Users className="h-3.5 w-3.5" />}
            label="Editors"
            value={`${war.editors.length} editor${war.editors.length !== 1 ? 's' : ''}`}
          />
          <StatCell
            icon={<FileEdit className="h-3.5 w-3.5" />}
            label="Edits"
            value={`${war.edit_count} edit${war.edit_count !== 1 ? 's' : ''}`}
          />
          <StatCell
            icon={<RotateCcw className="h-3.5 w-3.5" />}
            label="Reverts"
            value={`${war.revert_count} revert${war.revert_count !== 1 ? 's' : ''}`}
          />
          <StatCell
            icon={<Clock className="h-3.5 w-3.5" />}
            label="Duration"
            value={war.active ? `Active for ${durationSince(war.start_time)}` : durationSince(war.start_time)}
          />
        </div>

        {/* ── Editor list (avatars / names) ────────── */}
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          {war.editors.slice(0, 6).map((editor) => (
            <a
              key={editor}
              href={`https://en.wikipedia.org/wiki/User:${encodeURIComponent(editor.replace(/ /g, '_'))}`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 px-2 py-0.5 bg-white/80 border border-gray-200 rounded-full text-xs text-gray-700 hover:border-blue-300 hover:text-blue-700 transition-colors"
              title={editor}
            >
              <span className="w-4 h-4 rounded-full bg-gradient-to-br from-blue-400 to-purple-500 flex items-center justify-center text-[9px] text-white font-bold flex-shrink-0">
                {editor.charAt(0).toUpperCase()}
              </span>
              <span className="truncate max-w-[100px]">{editor}</span>
            </a>
          ))}
          {war.editors.length > 6 && (
            <span className="text-xs text-gray-500">
              +{war.editors.length - 6} more
            </span>
          )}
        </div>

        {/* ── Timeline indicator bar ──────────────── */}
        <TimelineBar war={war} />

        {/* ── Action buttons ──────────────────────── */}
        <div className="flex flex-wrap items-center gap-2 mt-3 pt-3 border-t border-gray-200/60">
          <a
            href={wikiUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium text-blue-700 bg-blue-50 hover:bg-blue-100 rounded transition-colors"
          >
            <ExternalLink className="h-3 w-3" />
            View page
          </a>

          <a
            href={historyUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium text-gray-700 bg-gray-100 hover:bg-gray-200 rounded transition-colors"
          >
            <History className="h-3 w-3" />
            Edit history
          </a>

          {/* Expand / collapse */}
          <button
            onClick={() => setExpanded(!expanded)}
            className="ml-auto inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium text-gray-600 hover:text-gray-800 hover:bg-gray-100 rounded transition-colors"
          >
            {expanded ? (
              <>
                <ChevronUp className="h-3 w-3" />
                Less
              </>
            ) : (
              <>
                <ChevronDown className="h-3 w-3" />
                Details
              </>
            )}
          </button>
        </div>
      </div>

      {/* ── Expanded details ──────────────────────── */}
      {expanded && (
        <div className="px-4 pb-4 border-t border-gray-200/60 animate-slide-down">
          <div className="mt-3 space-y-2 text-xs text-gray-600">
            <p>
              <span className="font-medium text-gray-700">Started:</span>{' '}
              {new Date(war.start_time).toLocaleString()} ({formatTimestamp(war.start_time)})
            </p>
            <p>
              <span className="font-medium text-gray-700">Last edit:</span>{' '}
              {new Date(war.last_edit).toLocaleString()} ({formatTimestamp(war.last_edit)})
            </p>
            <p>
              <span className="font-medium text-gray-700">Revert ratio:</span>{' '}
              {war.edit_count > 0
                ? `${((war.revert_count / war.edit_count) * 100).toFixed(0)}%`
                : 'N/A'}
            </p>

            {/* Full editor list */}
            <div>
              <span className="font-medium text-gray-700">All editors:</span>
              <ul className="mt-1 ml-4 list-disc space-y-0.5">
                {war.editors.map((editor) => (
                  <li key={editor}>
                    <a
                      href={`https://en.wikipedia.org/wiki/User:${encodeURIComponent(editor.replace(/ /g, '_'))}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-blue-600 hover:underline"
                    >
                      {editor}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          </div>

          {/* Editor Conflict Graph */}
          <div className="mt-4 pt-4 border-t border-gray-200/40">
            <EditorConflictGraph war={war} />
          </div>

          {/* Edit Timeline */}
          <div className="mt-4 pt-4 border-t border-gray-200/40">
            <EditWarTimeline war={war} />
          </div>
        </div>
      )}
    </div>
  );
}

/* ── Sub-components ──────────────────────────────────── */

function StatCell({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
}) {
  return (
    <div className="flex items-center gap-1.5 text-xs text-gray-600" title={label}>
      <span className="text-gray-400">{icon}</span>
      <span>{value}</span>
    </div>
  );
}

/**
 * A small visual bar showing relative activity over the edit-war window.
 * Uses start_time → now as the full width and plots a simple gradient.
 */
function TimelineBar({ war }: { war: EditWar }) {
  const startMs = new Date(war.start_time).getTime();
  const lastMs = new Date(war.last_edit).getTime();
  const nowMs = Date.now();
  const rangeMs = Math.max(nowMs - startMs, 1);
  const lastPct = Math.min(((lastMs - startMs) / rangeMs) * 100, 100);

  return (
    <div className="mt-3" aria-label="Activity timeline">
      <div className="flex items-center justify-between mb-1 text-[10px] text-gray-400">
        <span>Started</span>
        <span>{war.active ? 'Now' : 'Resolved'}</span>
      </div>
      <div className="h-1.5 w-full bg-gray-200 rounded-full overflow-hidden">
        <div
          className="h-full bg-gradient-to-r from-orange-400 to-red-500 rounded-full transition-all duration-500"
          style={{ width: `${lastPct}%` }}
        />
      </div>
    </div>
  );
}
