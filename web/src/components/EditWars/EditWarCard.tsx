import { useState } from 'react';
import type { EditWar, Side } from '../../types';
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
  Brain,
  ChevronDown,
  ChevronUp,
  Clock,
  ExternalLink,
  History,
  Lightbulb,
  RotateCcw,
  Shield,
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
 * Format a millisecond duration into a human-readable string.
 */
function formatDuration(diffMs: number): string {
  if (diffMs < 0) return 'just started';
  const mins = Math.floor(diffMs / 60_000);
  if (mins < 1) return 'less than a minute';
  if (mins < 60) return `${mins} minute${mins !== 1 ? 's' : ''}`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ${mins % 60}m`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

/**
 * Calculate the edit war span: last_edit − start_time.
 * Falls back to now − start_time if last_edit is unavailable.
 */
function warDuration(startIso: string, lastEditIso?: string): string {
  try {
    const start = new Date(startIso).getTime();
    const end = lastEditIso ? new Date(lastEditIso).getTime() : Date.now();
    return formatDuration(end - start);
  } catch {
    return 'unknown';
  }
}

export function EditWarCard({ war, onDismiss, isNew = false }: EditWarCardProps) {
  const [expanded, setExpanded] = useState(false);
  const severity = getSeverityColor(war.severity);
  const accentBorder =
    SEVERITY_BORDER[war.severity.toLowerCase()] ?? 'border-l-blue-400';
  const wikiUrl = buildWikiUrl(war.page_title, war.server_url);
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
            value={war.active ? `Active · ${warDuration(war.start_time, war.last_edit)}` : warDuration(war.start_time, war.last_edit)}
          />
        </div>

        {/* ── Editor list: grouped by side or flat fallback ────────── */}
        {war.analysis && war.analysis.sides && war.analysis.sides.length > 0 ? (
          <div className="mt-3 space-y-2">
            {war.analysis.sides.map((side: Side, sideIdx: number) => (
              <SideGroup key={sideIdx} side={side} sideIdx={sideIdx} serverUrl={war.server_url} />
            ))}
          </div>
        ) : (
          <div className="mt-3 flex flex-wrap items-center gap-1.5">
            {war.editors.slice(0, 6).map((editor) => (
              <a
                key={editor}
                href={buildWikiUrl(`User:${editor}`, war.server_url)}
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
        )}

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
          </div>

          {/* Inline Conflict Analysis (auto-loaded, no button) */}
          {war.analysis ? (
            <InlineAnalysis analysis={war.analysis} serverUrl={war.server_url} />
          ) : (
            <div
              className="mt-4 flex items-center gap-2 px-3 py-2 text-xs rounded border"
              style={{
                background: 'rgba(139, 92, 246, 0.05)',
                borderColor: 'rgba(139, 92, 246, 0.15)',
                color: 'rgba(139, 92, 246, 0.5)',
                fontFamily: 'monospace',
              }}
            >
              <Brain className="h-3.5 w-3.5" />
              <span>Analysis pending — will be available shortly after detection</span>
            </div>
          )}

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

/* ── Side colors for up to 4 sides ────────────────────── */
const SIDE_COLORS = [
  { accent: 'rgba(59, 130, 246, 0.5)', bg: 'rgba(59, 130, 246, 0.06)', border: 'rgba(59, 130, 246, 0.2)', gradient: 'from-blue-700 to-cyan-700' },
  { accent: 'rgba(239, 68, 68, 0.5)', bg: 'rgba(239, 68, 68, 0.06)', border: 'rgba(239, 68, 68, 0.2)', gradient: 'from-red-700 to-orange-700' },
  { accent: 'rgba(234, 179, 8, 0.5)', bg: 'rgba(234, 179, 8, 0.06)', border: 'rgba(234, 179, 8, 0.2)', gradient: 'from-yellow-700 to-amber-700' },
  { accent: 'rgba(168, 85, 247, 0.5)', bg: 'rgba(168, 85, 247, 0.06)', border: 'rgba(168, 85, 247, 0.2)', gradient: 'from-purple-700 to-pink-700' },
];

/**
 * Displays a group of editors belonging to one side of the conflict.
 */
function SideGroup({
  side,
  sideIdx,
  serverUrl,
}: {
  side: Side;
  sideIdx: number;
  serverUrl?: string;
}) {
  const colors = SIDE_COLORS[sideIdx % SIDE_COLORS.length];
  return (
    <div
      className="rounded px-2.5 py-2"
      style={{
        background: colors.bg,
        borderLeft: `3px solid ${colors.accent}`,
      }}
    >
      <div
        className="text-[10px] font-semibold uppercase tracking-wide mb-1.5 flex items-center gap-1"
        style={{ color: colors.accent, fontFamily: 'monospace' }}
      >
        <Swords className="h-3 w-3" />
        {side.position}
      </div>
      <div className="flex flex-wrap gap-1.5">
        {side.editors.map((editor) => (
          <a
            key={editor.user}
            href={buildWikiUrl(`User:${editor.user}`, serverUrl)}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 px-2 py-0.5 bg-monitor-card border border-monitor-border rounded-full text-xs transition-colors text-monitor-text"
            title={`${editor.user} · ${editor.edit_count} edits · ${editor.role}`}
          >
            <span className={`w-4 h-4 rounded-full bg-gradient-to-br ${colors.gradient} flex items-center justify-center text-[9px] text-white font-bold flex-shrink-0`}>
              {editor.user.charAt(0).toUpperCase()}
            </span>
            <span className="truncate max-w-[100px]">{editor.user}</span>
            <span className="text-[9px] opacity-50">{editor.edit_count}e</span>
          </a>
        ))}
      </div>
    </div>
  );
}

/**
 * Inline analysis panel — rendered automatically when analysis data is present.
 * No manual button needed.
 */
function InlineAnalysis({
  analysis,
  serverUrl,
}: {
  analysis: import('../../types').EditWarAnalysis;
  serverUrl?: string;
}) {
  return (
    <div className="mt-4 space-y-3" style={{ borderTop: '1px solid rgba(139,92,246,0.1)', paddingTop: '1rem' }}>
      {/* Header */}
      <div
        className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide"
        style={{ color: '#a78bfa', fontFamily: 'monospace' }}
      >
        <Brain className="h-3.5 w-3.5" />
        <span>Conflict Analysis</span>
        {analysis.content_area && analysis.content_area !== 'unknown' && (
          <span
            className="text-[10px] font-medium px-2 py-0.5 rounded-full"
            style={{
              background: 'rgba(139, 92, 246, 0.1)',
              color: '#c4b5fd',
              border: '1px solid rgba(139, 92, 246, 0.2)',
            }}
          >
            {analysis.content_area}
          </span>
        )}
        <span
          className="text-[10px] px-2 py-0.5 rounded-full"
          style={{
            background: 'rgba(0, 255, 136, 0.05)',
            color: 'rgba(0, 255, 136, 0.4)',
            border: '1px solid rgba(0, 255, 136, 0.1)',
          }}
        >
          {analysis.edit_count} edits analyzed
        </span>
      </div>

      {/* Summary */}
      <p
        className="text-xs leading-relaxed"
        style={{ color: 'rgba(0, 255, 136, 0.7)', fontFamily: 'monospace' }}
      >
        {analysis.summary}
      </p>

      {/* Recommendation */}
      {analysis.recommendation && (
        <div
          className="flex items-start gap-2 px-2.5 py-2 rounded text-[11px] leading-snug"
          style={{
            background: 'rgba(59, 130, 246, 0.06)',
            border: '1px solid rgba(59, 130, 246, 0.15)',
            color: 'rgba(147, 197, 253, 0.8)',
            fontFamily: 'monospace',
          }}
        >
          <Shield className="h-3.5 w-3.5 mt-0.5 shrink-0" style={{ color: 'rgba(59, 130, 246, 0.6)' }} />
          <span>{analysis.recommendation}</span>
        </div>
      )}

      {/* Opposing sides (detailed) */}
      {analysis.sides && analysis.sides.length > 0 && (
        <div className="space-y-1.5">
          <h5
            className="text-[10px] font-semibold uppercase tracking-wide flex items-center gap-1"
            style={{ color: 'rgba(139, 92, 246, 0.6)', fontFamily: 'monospace' }}
          >
            <Lightbulb className="h-3 w-3" />
            Opposing Sides
          </h5>
          <div className="space-y-2">
            {analysis.sides.map((side, idx) => (
              <SideGroup key={idx} side={side} sideIdx={idx} serverUrl={serverUrl} />
            ))}
          </div>
        </div>
      )}

      {/* Generated time */}
      <div
        className="text-[9px] text-right"
        style={{ color: 'rgba(0, 255, 136, 0.2)', fontFamily: 'monospace' }}
      >
        {analysis.cache_hit ? 'cached · ' : ''}
        generated {new Date(analysis.generated_at).toLocaleTimeString()}
      </div>
    </div>
  );
}
