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
  high: 'border-l-amber-500',
  medium: 'border-l-emerald-500',
  low: 'border-l-cyan-500',
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
                className="text-sm font-bold hover:underline leading-tight block truncate"
                style={{ color: '#00ff88' }}
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
                      ? 'bg-red-900/10 text-red-300 border border-red-700/20'
                      : 'bg-monitor-card/8 text-monitor-text-dim border border-monitor-border/20'
                  }`}
                >
                  <span
                    className={`inline-block w-1.5 h-1.5 rounded-full ${
                      war.active ? 'bg-red-500 animate-pulse' : 'bg-monitor-text-dim'
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
              className="p-1 rounded opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
              style={{ color: 'rgba(0,255,136,0.4)' }}
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
              className="inline-flex items-center gap-1 px-2 py-0.5 bg-monitor-card border border-monitor-border rounded-full text-xs transition-colors text-monitor-text"
              title={editor}
            >
              <span className="w-4 h-4 rounded-full bg-gradient-to-br from-blue-700 to-purple-700 flex items-center justify-center text-[9px] text-white font-bold flex-shrink-0">
                {editor.charAt(0).toUpperCase()}
              </span>
              <span className="truncate max-w-[100px]">{editor}</span>
            </a>
          ))}
          {war.editors.length > 6 && (
            <span className="text-xs text-monitor-text-dim">
              +{war.editors.length - 6} more
            </span>
          )}
        </div>

        {/* ── Timeline indicator bar ──────────────── */}
        <TimelineBar war={war} />

        {/* ── Action buttons ──────────────────────── */}
        <div className="flex flex-wrap items-center gap-2 mt-3 pt-3" style={{ borderTop: '1px solid rgba(0,255,136,0.08)' }}>
          <a
            href={wikiUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded transition-colors"
            style={{ background: 'rgba(0,255,136,0.1)', color: '#00ff88', fontFamily: 'monospace' }}
          >
            <ExternalLink className="h-3 w-3" />
            VIEW PAGE
          </a>

          <a
            href={historyUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded transition-colors"
            style={{ background: 'rgba(0,221,255,0.1)', color: '#00ddff', fontFamily: 'monospace' }}
          >
            <History className="h-3 w-3" />
            Edit history
          </a>

          {/* Expand / collapse */}
          <button
            onClick={() => setExpanded(!expanded)}
            className="ml-auto inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium rounded transition-colors"
            style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}
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
        <div className="px-4 pb-4 animate-slide-down" style={{ borderTop: '1px solid rgba(0,255,136,0.08)' }}>
          <div className="mt-3 space-y-2 text-xs" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}>
            <p>
              <span className="font-medium" style={{ color: 'rgba(0,255,136,0.7)' }}>Started:</span>{' '}
              {new Date(war.start_time).toLocaleString()} ({formatTimestamp(war.start_time)})
            </p>
            {war.last_edit && (
              <p>
                <span className="font-medium" style={{ color: 'rgba(0,255,136,0.7)' }}>Last edit:</span>{' '}
                {new Date(war.last_edit).toLocaleString()} ({formatTimestamp(war.last_edit)})
              </p>
            )}
            <p>
              <span className="font-medium" style={{ color: 'rgba(0,255,136,0.7)' }}>Revert ratio:</span>{' '}
              {war.edit_count > 0
                ? `${((war.revert_count / war.edit_count) * 100).toFixed(0)}%`
                : 'N/A'}
            </p>

            {/* Full editor list */}
            <div>
              <span className="font-medium" style={{ color: 'rgba(0,255,136,0.7)' }}>All editors:</span>
              <ul className="mt-1 ml-4 list-disc space-y-0.5">
                {war.editors.map((editor) => (
                  <li key={editor}>
                    <a
                      href={`https://en.wikipedia.org/wiki/User:${encodeURIComponent(editor.replace(/ /g, '_'))}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-monitor-text"
                    >
                      {editor}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          </div>

          {/* Editor Conflict Graph */}
          <div className="mt-4 pt-4" style={{ borderTop: '1px solid rgba(0,255,136,0.06)' }}>
            <EditorConflictGraph war={war} />
          </div>

          {/* Edit Timeline */}
          <div className="mt-4 pt-4" style={{ borderTop: '1px solid rgba(0,255,136,0.06)' }}>
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
    <div className="flex items-center gap-1.5 text-xs" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }} title={label}>
      <span style={{ color: 'rgba(0,255,136,0.3)' }}>{icon}</span>
      <span>{value}</span>
    </div>
  );
}

/**
 * A small visual bar showing relative activity over the edit-war window.
 * Uses start_time → now as the full width and plots a simple gradient.
 */
function TimelineBar({ war }: { war: EditWar }) {
  const startMs = war.start_time ? new Date(war.start_time).getTime() : Date.now();
  const lastMs = war.last_edit ? new Date(war.last_edit).getTime() : Date.now();
  const nowMs = Date.now();
  const rangeMs = Math.max(nowMs - startMs, 1);
  const validStart = Number.isFinite(startMs);
  const validLast = Number.isFinite(lastMs);
  const lastPct = validStart && validLast
    ? Math.min(Math.max(((lastMs - startMs) / rangeMs) * 100, 0), 100)
    : 100;

  return (
    <div className="mt-3" aria-label="Activity timeline">
      <div className="flex items-center justify-between mb-1 text-[10px]" style={{ color: 'rgba(0,255,136,0.3)', fontFamily: 'monospace' }}>
        <span>STARTED</span>
        <span>{war.active ? 'NOW' : 'RESOLVED'}</span>
      </div>
      <div className="h-1.5 w-full rounded-full overflow-hidden" style={{ background: 'rgba(0,255,136,0.06)' }}>
        <div
          className="h-full rounded-full transition-all duration-500"
          style={{ width: `${lastPct}%`, background: war.active ? 'linear-gradient(to right, #ffaa00, #ff4444)' : 'linear-gradient(to right, rgba(0,255,136,0.3), rgba(0,255,136,0.5))' }}
        />
      </div>
    </div>
  );
}
