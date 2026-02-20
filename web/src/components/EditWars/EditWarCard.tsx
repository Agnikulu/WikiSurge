import { useState, useEffect, useRef } from 'react';
import type { EditWar, Side } from '../../types';
import {
  buildWikiUrl,
  formatTimestamp,
  getSeverityColor,
  truncateTitle,
} from '../../utils/formatting';
import { SeverityBadge } from '../Alerts/SeverityBadge';
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
  Target,
  Users,
  X,
  FileEdit,
  Zap,
  Sparkles,
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
  if (diffMs <= 0) return 'less than a minute';
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
function warDuration(startIso: string, lastEditIso?: string, active?: boolean): string {
  try {
    const start = new Date(startIso).getTime();
    const end = lastEditIso ? new Date(lastEditIso).getTime() : Date.now();
    const dur = formatDuration(end - start);
    if (active) return `Active · ${dur}`;
    return dur;
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
            value={warDuration(war.start_time, war.last_edit, war.active)}
          />
        </div>

        {/* ── Editor pills (always flat — detailed sides shown in analysis) ── */}
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          {war.editors.slice(0, 6).map((editor) => (
            <a
              key={editor}
              href={buildWikiUrl(`User:${editor}`, war.server_url)}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 px-2 py-0.5 bg-monitor-card border border-monitor-border rounded-full text-xs transition-colors text-monitor-text hover:border-monitor-green/30"
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
 * Typewriter hook — reveals text character-by-character.
 * Only animates once; subsequent renders with the same text show it instantly.
 */
function useTypewriter(text: string, speed = 12, enabled = true) {
  const [displayed, setDisplayed] = useState(enabled ? '' : text);
  const [done, setDone] = useState(!enabled);
  const prevText = useRef(text);

  useEffect(() => {
    // If the text changed (re-analysis), restart the animation
    if (text !== prevText.current) {
      prevText.current = text;
      if (enabled) {
        setDisplayed('');
        setDone(false);
      } else {
        setDisplayed(text);
        setDone(true);
      }
    }
  }, [text, enabled]);

  useEffect(() => {
    if (!enabled || done) return;
    if (displayed.length >= text.length) {
      setDone(true);
      return;
    }
    // Batch 2-3 chars per tick for a fast but visible stream
    const batch = Math.min(3, text.length - displayed.length);
    const timer = setTimeout(() => {
      setDisplayed(text.slice(0, displayed.length + batch));
    }, speed);
    return () => clearTimeout(timer);
  }, [displayed, text, speed, enabled, done]);

  return { displayed, done };
}

/**
 * VS-style matchup layout — the visual centrepiece of the analysis.
 * Two opposing sides shown side-by-side with a glowing VS divider.
 * Each editor gets a card showing their LLM-assigned role.
 */
function VSMatchup({ sides, serverUrl }: { sides: Side[]; serverUrl?: string }) {
  const leftSide = sides[0];
  const rightSide = sides[1];
  const extraSides = sides.slice(2);
  const leftColors = SIDE_COLORS[0];
  const rightColors = SIDE_COLORS[1];

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-[1fr,auto,1fr] gap-3 items-stretch">
        {/* Left side */}
        <SidePanel side={leftSide} colors={leftColors} serverUrl={serverUrl} />

        {/* VS divider */}
        <div className="flex flex-col items-center justify-center px-0.5">
          <div
            className="w-10 h-10 rounded-full flex items-center justify-center animate-pulse-border"
            style={{
              background: 'rgba(255, 100, 50, 0.12)',
              border: '2px solid rgba(255, 100, 50, 0.35)',
              boxShadow: '0 0 20px rgba(255, 100, 50, 0.15)',
            }}
          >
            <Zap className="h-4 w-4" style={{ color: '#ff6432' }} />
          </div>
          <span
            className="text-[9px] font-black mt-1 tracking-widest"
            style={{ color: 'rgba(255, 100, 50, 0.5)', fontFamily: 'monospace' }}
          >
            VS
          </span>
        </div>

        {/* Right side */}
        <SidePanel side={rightSide} colors={rightColors} serverUrl={serverUrl} />
      </div>

      {/* Extra sides beyond 2 */}
      {extraSides.length > 0 && (
        <div className="space-y-2">
          {extraSides.map((side, idx) => (
            <SideGroup key={idx + 2} side={side} sideIdx={idx + 2} serverUrl={serverUrl} />
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * One side of the VS matchup — position label + editor cards with roles.
 */
function SidePanel({
  side,
  colors,
  serverUrl,
}: {
  side: Side;
  colors: typeof SIDE_COLORS[number];
  serverUrl?: string;
}) {
  return (
    <div
      className="rounded-lg overflow-hidden"
      style={{
        background: colors.bg,
        border: `1px solid ${colors.border}`,
      }}
    >
      {/* Position header */}
      <div
        className="px-3 py-2"
        style={{ borderBottom: `1px solid ${colors.border}` }}
      >
        <p
          className="text-[11px] font-medium leading-snug"
          style={{ color: colors.accent, fontFamily: 'monospace' }}
        >
          {side.position}
        </p>
      </div>

      {/* Editor cards */}
      <div className="p-2 space-y-1.5">
        {side.editors.map((editor) => (
          <a
            key={editor.user}
            href={buildWikiUrl(`User:${editor.user}`, serverUrl)}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-2 px-2 py-1.5 rounded-md transition-colors hover:bg-white/5 group/editor"
          >
            <span
              className={`w-7 h-7 rounded-full bg-gradient-to-br ${colors.gradient} flex items-center justify-center text-[11px] text-white font-bold flex-shrink-0 shadow-sm`}
            >
              {editor.user.charAt(0).toUpperCase()}
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <span
                  className="text-xs font-semibold truncate group-hover/editor:underline"
                  style={{ color: 'rgba(255,255,255,0.85)' }}
                >
                  {editor.user}
                </span>
                <span
                  className="text-[9px] px-1.5 py-0.5 rounded-full flex-shrink-0"
                  style={{
                    background: `${colors.accent}20`,
                    color: colors.accent,
                    border: `1px solid ${colors.border}`,
                  }}
                >
                  {editor.edit_count} edits
                </span>
              </div>
              <p
                className="text-[10px] leading-tight mt-0.5 truncate"
                style={{ color: 'rgba(255,255,255,0.4)', fontFamily: 'monospace' }}
                title={editor.role}
              >
                {editor.role}
              </p>
            </div>
          </a>
        ))}
      </div>
    </div>
  );
}

/**
 * Inline analysis panel — rendered automatically when analysis data is present.
 * Clean, no-accordion layout: narrative → content area → VS matchup → recommendation.
 */
function InlineAnalysis({
  analysis,
  serverUrl,
}: {
  analysis: import('../../types').EditWarAnalysis;
  serverUrl?: string;
}) {
  const shouldAnimate = !analysis.cache_hit;
  const { displayed: summaryText, done: typingDone } = useTypewriter(
    analysis.summary,
    12,
    shouldAnimate,
  );

  return (
    <div
      className="mt-4 space-y-4 animate-fade-in"
      style={{ borderTop: '1px solid rgba(139,92,246,0.12)', paddingTop: '1rem' }}
    >
      {/* ── AI header badge ── */}
      <div className="flex items-center justify-between">
        <div
          className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full"
          style={{
            background: 'linear-gradient(135deg, rgba(139,92,246,0.15), rgba(59,130,246,0.1))',
            border: '1px solid rgba(139,92,246,0.25)',
          }}
        >
          <Sparkles className="h-3 w-3" style={{ color: '#a78bfa' }} />
          <span
            className="text-[10px] font-semibold uppercase tracking-wider"
            style={{ color: '#c4b5fd', fontFamily: 'monospace' }}
          >
            AI Analysis
          </span>
          <span
            className="text-[9px] opacity-50"
            style={{ color: '#c4b5fd', fontFamily: 'monospace' }}
          >
            · {analysis.edit_count} edits
          </span>
        </div>
        <span
          className="text-[9px]"
          style={{ color: 'rgba(0, 255, 136, 0.2)', fontFamily: 'monospace' }}
        >
          {analysis.cache_hit ? 'cached · ' : ''}
          {new Date(analysis.generated_at).toLocaleTimeString()}
        </span>
      </div>

      {/* ── Narrative summary ── */}
      <div
        className="rounded-lg px-4 py-3 relative overflow-hidden"
        style={{
          background: 'linear-gradient(135deg, rgba(139,92,246,0.06), rgba(59,130,246,0.03))',
          border: '1px solid rgba(139,92,246,0.15)',
        }}
      >
        {/* Subtle gradient accent line at top */}
        <div
          className="absolute top-0 left-0 right-0 h-[2px]"
          style={{ background: 'linear-gradient(90deg, #a78bfa, #3b82f6, #00ff88)' }}
        />
        <p
          className="text-[12px] leading-[1.7]"
          style={{ color: 'rgba(255,255,255,0.78)', fontFamily: 'monospace' }}
        >
          {summaryText}
          {!typingDone && (
            <span
              className="inline-block w-1.5 h-4 ml-0.5 align-text-bottom animate-cursor-blink"
              style={{ background: '#a78bfa' }}
            />
          )}
        </p>
      </div>

      {/* ── "What they're fighting over" callout ── */}
      {analysis.content_area && analysis.content_area !== 'unknown' && (
        <div
          className="flex items-start gap-2.5 rounded-lg px-3.5 py-2.5"
          style={{
            background: 'rgba(234, 179, 8, 0.06)',
            border: '1px solid rgba(234, 179, 8, 0.15)',
          }}
        >
          <Target
            className="h-4 w-4 mt-0.5 flex-shrink-0"
            style={{ color: 'rgba(234, 179, 8, 0.6)' }}
          />
          <div>
            <span
              className="text-[10px] font-semibold uppercase tracking-wide block mb-0.5"
              style={{ color: 'rgba(234, 179, 8, 0.5)', fontFamily: 'monospace' }}
            >
              What they're fighting over
            </span>
            <span
              className="text-[12px] font-medium"
              style={{ color: 'rgba(234, 179, 8, 0.85)', fontFamily: 'monospace' }}
            >
              {analysis.content_area}
            </span>
          </div>
        </div>
      )}

      {/* ── Opposing sides (VS matchup — always visible) ── */}
      {analysis.sides && analysis.sides.length > 0 && (
        <div>
          <div
            className="flex items-center gap-1.5 mb-2.5"
          >
            <Swords className="h-3.5 w-3.5" style={{ color: 'rgba(255,100,50,0.5)' }} />
            <span
              className="text-[10px] font-semibold uppercase tracking-wider"
              style={{ color: 'rgba(255,100,50,0.5)', fontFamily: 'monospace' }}
            >
              Opposing Sides
            </span>
          </div>
          {analysis.sides.length >= 2 ? (
            <VSMatchup sides={analysis.sides} serverUrl={serverUrl} />
          ) : (
            <div className="space-y-2">
              {analysis.sides.map((side, idx) => (
                <SideGroup key={idx} side={side} sideIdx={idx} serverUrl={serverUrl} />
              ))}
            </div>
          )}
        </div>
      )}

      {/* ── Recommendation action card ── */}
      {analysis.recommendation && (
        <div
          className="rounded-lg overflow-hidden"
          style={{
            border: '1px solid rgba(59, 130, 246, 0.2)',
          }}
        >
          {/* Header strip */}
          <div
            className="flex items-center gap-1.5 px-3.5 py-2"
            style={{
              background: 'linear-gradient(135deg, rgba(59,130,246,0.12), rgba(139,92,246,0.08))',
              borderBottom: '1px solid rgba(59,130,246,0.15)',
            }}
          >
            <Shield className="h-3.5 w-3.5" style={{ color: 'rgba(96, 165, 250, 0.7)' }} />
            <span
              className="text-[10px] font-semibold uppercase tracking-wider"
              style={{ color: 'rgba(96, 165, 250, 0.7)', fontFamily: 'monospace' }}
            >
              Recommended Action
            </span>
          </div>
          {/* Body */}
          <div
            className="px-3.5 py-3"
            style={{ background: 'rgba(59, 130, 246, 0.04)' }}
          >
            <div className="flex items-start gap-2.5">
              <Lightbulb
                className="h-4 w-4 mt-0.5 flex-shrink-0"
                style={{ color: 'rgba(250, 204, 21, 0.6)' }}
              />
              <p
                className="text-[12px] leading-[1.7]"
                style={{
                  color: 'rgba(191, 219, 254, 0.85)',
                  fontFamily: 'monospace',
                }}
              >
                {analysis.recommendation}
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
