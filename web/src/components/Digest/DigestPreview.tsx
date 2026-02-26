import { useState } from 'react';
import {
  Calendar,
  CalendarDays,
  Flame,
  Swords,
  TrendingUp,
  Bookmark,
  BarChart3,
  Globe,
  Users,
  RotateCcw,
  ChevronDown,
  ChevronUp,
  ExternalLink,
  Zap,
  Eye,
} from 'lucide-react';
import type { DigestData, GlobalHighlight, WatchlistEvent } from '../../types/digest';
import { mockDailyDigest, mockWeeklyDigest } from '../../data/mockDigest';

// -------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------

function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(n >= 10_000 ? 0 : 1) + 'K';
  return n.toLocaleString();
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  });
}

function formatDateRange(start: string, end: string): string {
  const s = new Date(start);
  const e = new Date(end);
  const sStr = s.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  const eStr = e.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
  return `${sStr} – ${eStr}`;
}

function severityColor(severity?: string): string {
  switch (severity) {
    case 'critical':
      return '#ef4444';
    case 'high':
      return '#f59e0b';
    case 'moderate':
      return '#3b82f6';
    case 'low':
      return '#22c55e';
    default:
      return '#8b949e';
  }
}

function severityBg(severity?: string): string {
  switch (severity) {
    case 'critical':
      return 'rgba(239,68,68,0.08)';
    case 'high':
      return 'rgba(245,158,11,0.08)';
    case 'moderate':
      return 'rgba(59,130,246,0.08)';
    case 'low':
      return 'rgba(34,197,94,0.08)';
    default:
      return 'rgba(139,148,158,0.05)';
  }
}

function eventTypeIcon(type: string) {
  switch (type) {
    case 'edit_war':
      return <Swords className="h-3.5 w-3.5" style={{ color: '#ef4444' }} />;
    case 'trending':
      return <TrendingUp className="h-3.5 w-3.5" style={{ color: '#8b5cf6' }} />;
    case 'spike':
      return <Zap className="h-3.5 w-3.5" style={{ color: '#f59e0b' }} />;
    default:
      return <Eye className="h-3.5 w-3.5" style={{ color: '#8b949e' }} />;
  }
}

function rankEmoji(rank: number): string {
  switch (rank) {
    case 1:
      return '🥇';
    case 2:
      return '🥈';
    case 3:
      return '🥉';
    default:
      return `#${rank}`;
  }
}

function articleUrl(title: string, serverUrl?: string): string {
  const base = serverUrl || 'https://en.wikipedia.org';
  return `${base}/wiki/${encodeURIComponent(title.replace(/ /g, '_'))}`;
}

// -------------------------------------------------------------------
// Sub-components
// -------------------------------------------------------------------

function SectionDivider({ color = '#30363d' }: { color?: string }) {
  return (
    <div
      className="my-6"
      style={{ height: 1, background: color }}
    />
  );
}

function StatCard({
  value,
  label,
  icon,
  color,
  bg,
}: {
  value: string | number;
  label: string;
  icon: React.ReactNode;
  color: string;
  bg: string;
}) {
  return (
    <div
      className="flex-1 rounded-2xl p-5 text-center"
      style={{ background: bg, border: `1px solid ${color}22` }}
    >
      <div className="flex justify-center mb-2 opacity-60">{icon}</div>
      <p className="text-3xl sm:text-4xl font-extrabold font-mono" style={{ color }}>
        {typeof value === 'number' ? formatNumber(value) : value}
      </p>
      <p className="text-[11px] font-mono uppercase tracking-widest mt-1" style={{ color: '#8b949e' }}>
        {label}
      </p>
    </div>
  );
}

function LanguageBar({ language, percentage }: { language: string; percentage: number }) {
  return (
    <div className="flex items-center gap-3 mb-2">
      <span className="text-xs font-mono font-bold w-8 text-right" style={{ color: '#e6edf3' }}>
        {language.toUpperCase()}
      </span>
      <div className="flex-1 h-2.5 rounded-full" style={{ background: '#21262d' }}>
        <div
          className="h-full rounded-full transition-all duration-700"
          style={{
            width: `${Math.min(percentage * 2.5, 100)}%`,
            background: 'linear-gradient(90deg, #8b5cf6, #00ff88)',
          }}
        />
      </div>
      <span className="text-[11px] font-mono w-12 text-right" style={{ color: '#8b949e' }}>
        {percentage.toFixed(1)}%
      </span>
    </div>
  );
}

function EditWarCard({ item }: { item: GlobalHighlight }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div
      className="rounded-xl overflow-hidden transition-all"
      style={{
        background: '#0d1117',
        border: `1px solid ${severityColor(item.severity)}33`,
      }}
    >
      {/* Intensity accent bar */}
      <div className="h-[3px]" style={{ background: severityColor(item.severity) }} />

      <div className="p-4">
        {/* Header row */}
        <div className="flex items-start gap-3">
          <span className="text-2xl leading-none mt-0.5">{rankEmoji(item.rank)}</span>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <a
                href={articleUrl(item.title, item.server_url)}
                target="_blank"
                rel="noopener noreferrer"
                className="text-base font-bold hover:underline flex items-center gap-1"
                style={{ color: '#e6edf3' }}
              >
                <Swords className="h-4 w-4 inline" style={{ color: '#ef4444' }} />
                {item.title}
                <ExternalLink className="h-3 w-3 opacity-40" />
              </a>
              {item.severity && (
                <span
                  className="text-[10px] font-mono font-bold uppercase px-2 py-0.5 rounded-full"
                  style={{
                    color: severityColor(item.severity),
                    background: severityBg(item.severity),
                    border: `1px solid ${severityColor(item.severity)}44`,
                  }}
                >
                  {item.severity}
                </span>
              )}
            </div>

            {/* Meta row */}
            <div className="flex items-center gap-4 mt-2 text-[11px] font-mono" style={{ color: '#8b949e' }}>
              {item.edit_count > 0 && (
                <span className="flex items-center gap-1">
                  <Flame className="h-3 w-3" style={{ color: severityColor(item.severity) }} />
                  <strong style={{ color: severityColor(item.severity) }}>{formatNumber(item.edit_count)}</strong> edits
                </span>
              )}
              {item.editor_count && (
                <span className="flex items-center gap-1">
                  <Users className="h-3 w-3" />
                  {item.editor_count} editors
                </span>
              )}
              {item.revert_count && (
                <span className="flex items-center gap-1">
                  <RotateCcw className="h-3 w-3" />
                  {item.revert_count} reverts
                </span>
              )}
            </div>
          </div>

          <div className="text-right shrink-0">
            <p className="text-xl font-extrabold font-mono" style={{ color: severityColor(item.severity) }}>
              {formatNumber(item.edit_count)}
            </p>
            <p className="text-[10px] font-mono uppercase tracking-wider" style={{ color: '#8b949e' }}>
              edits
            </p>
          </div>
        </div>

        {/* LLM Summary */}
        {item.llm_summary && (
          <div
            className="mt-3 rounded-lg"
            style={{
              background: '#161b22',
              borderLeft: '3px solid #8b5cf6',
            }}
          >
            <div className="px-4 py-3">
              <p className="text-[13px] leading-relaxed" style={{ color: '#c9d1d9' }}>
                {expanded || item.llm_summary.length <= 180
                  ? item.llm_summary
                  : item.llm_summary.slice(0, 180) + '...'}
              </p>
              {item.llm_summary.length > 180 && (
                <button
                  onClick={() => setExpanded(!expanded)}
                  className="flex items-center gap-1 mt-1.5 text-[11px] font-mono font-semibold"
                  style={{ color: '#8b5cf6' }}
                >
                  {expanded ? (
                    <>
                      Show less <ChevronUp className="h-3 w-3" />
                    </>
                  ) : (
                    <>
                      Read more <ChevronDown className="h-3 w-3" />
                    </>
                  )}
                </button>
              )}
            </div>
          </div>
        )}

        {/* Content area + editors */}
        {(item.content_area || (item.editors && item.editors.length > 0)) && (
          <div className="flex flex-wrap gap-2 mt-3 text-[10px] font-mono" style={{ color: '#8b949e' }}>
            {item.content_area && (
              <span
                className="px-2 py-0.5 rounded-md"
                style={{ background: 'rgba(139,92,246,0.1)', color: '#a78bfa' }}
              >
                📋 {item.content_area}
              </span>
            )}
            {item.editors && item.editors.slice(0, 4).map((e) => (
              <span
                key={e}
                className="px-2 py-0.5 rounded-md"
                style={{ background: 'rgba(0,255,136,0.05)', color: '#6b7280' }}
              >
                @{e}
              </span>
            ))}
            {item.editors && item.editors.length > 4 && (
              <span className="px-2 py-0.5 rounded-md" style={{ color: '#6b7280' }}>
                +{item.editors.length - 4} more
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function TrendingCard({ item }: { item: GlobalHighlight }) {
  return (
    <div
      className="rounded-xl p-4 flex items-start gap-3 transition-all"
      style={{
        background: '#0d1117',
        border: '1px solid rgba(139,92,246,0.2)',
      }}
    >
      <span className="text-2xl leading-none mt-0.5">{rankEmoji(item.rank)}</span>
      <div className="flex-1 min-w-0">
        <a
          href={articleUrl(item.title, item.server_url)}
          target="_blank"
          rel="noopener noreferrer"
          className="text-base font-bold hover:underline flex items-center gap-1"
          style={{ color: '#e6edf3' }}
        >
          <TrendingUp className="h-4 w-4 inline" style={{ color: '#8b5cf6' }} />
          {item.title}
          <ExternalLink className="h-3 w-3 opacity-40" />
        </a>
        <p className="text-[12px] mt-1 leading-relaxed" style={{ color: '#8b949e' }}>
          {item.summary}
        </p>
      </div>
      <div className="text-right shrink-0">
        <p className="text-lg font-extrabold font-mono" style={{ color: '#8b5cf6' }}>
          {formatNumber(item.edit_count)}
        </p>
        <p className="text-[10px] font-mono uppercase tracking-wider" style={{ color: '#8b949e' }}>
          edits
        </p>
      </div>
    </div>
  );
}

function WatchlistItem({ item }: { item: WatchlistEvent }) {
  const isNotable = item.is_notable;
  const borderColor = isNotable
    ? item.event_type === 'edit_war'
      ? 'rgba(239,68,68,0.3)'
      : 'rgba(139,92,246,0.3)'
    : 'rgba(139,148,158,0.1)';
  const accentColor = isNotable
    ? item.event_type === 'edit_war'
      ? '#ef4444'
      : '#8b5cf6'
    : '#8b949e';

  return (
    <div
      className="rounded-lg p-3 flex items-center gap-3 transition-all"
      style={{
        background: isNotable ? '#0d1117' : 'rgba(13,17,23,0.5)',
        border: `1px solid ${borderColor}`,
        opacity: isNotable ? 1 : 0.7,
      }}
    >
      {eventTypeIcon(item.event_type)}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold" style={{ color: '#e6edf3' }}>
            {item.title}
          </span>
          {isNotable && (
            <span
              className="text-[9px] font-mono font-bold uppercase px-1.5 py-0.5 rounded"
              style={{ background: `${accentColor}15`, color: accentColor, border: `1px solid ${accentColor}33` }}
            >
              Notable
            </span>
          )}
          {item.spike_ratio && item.spike_ratio > 1 && (
            <span className="text-[10px] font-mono" style={{ color: '#f59e0b' }}>
              {item.spike_ratio.toFixed(1)}x
            </span>
          )}
        </div>
        <p className="text-[11px] mt-0.5" style={{ color: '#8b949e' }}>
          {item.summary}
        </p>
      </div>
      <div className="text-right shrink-0">
        <span className="text-sm font-bold font-mono" style={{ color: accentColor }}>
          {formatNumber(item.edit_count)}
        </span>
      </div>
    </div>
  );
}

// -------------------------------------------------------------------
// Main Component
// -------------------------------------------------------------------

export function DigestPreview() {
  const [period, setPeriod] = useState<'daily' | 'weekly'>('daily');
  const data: DigestData = period === 'daily' ? mockDailyDigest : mockWeeklyDigest;

  const dateLabel =
    period === 'daily'
      ? formatDate(data.period_start)
      : formatDateRange(data.period_start, data.period_end);

  const notableWatchlist = data.watchlist_events.filter((e) => e.is_notable);
  const quietWatchlist = data.watchlist_events.filter((e) => !e.is_notable);

  return (
    <div className="max-w-2xl mx-auto">
      {/* Period Toggle */}
      <div className="flex items-center justify-center gap-3 mb-6">
        <button
          onClick={() => setPeriod('daily')}
          className="flex items-center gap-2 px-5 py-2.5 rounded-xl text-sm font-mono font-bold transition-all"
          style={{
            background: period === 'daily' ? 'rgba(0,255,136,0.12)' : 'rgba(0,255,136,0.02)',
            border: `1px solid ${period === 'daily' ? 'rgba(0,255,136,0.4)' : 'rgba(0,255,136,0.1)'}`,
            color: period === 'daily' ? '#00ff88' : 'rgba(0,255,136,0.4)',
          }}
        >
          <Calendar className="h-4 w-4" />
          DAILY
        </button>
        <button
          onClick={() => setPeriod('weekly')}
          className="flex items-center gap-2 px-5 py-2.5 rounded-xl text-sm font-mono font-bold transition-all"
          style={{
            background: period === 'weekly' ? 'rgba(139,92,246,0.12)' : 'rgba(139,92,246,0.02)',
            border: `1px solid ${period === 'weekly' ? 'rgba(139,92,246,0.4)' : 'rgba(139,92,246,0.1)'}`,
            color: period === 'weekly' ? '#a78bfa' : 'rgba(139,92,246,0.4)',
          }}
        >
          <CalendarDays className="h-4 w-4" />
          WEEKLY
        </button>
      </div>

      {/* ============================================ */}
      {/* HERO CARD                                    */}
      {/* ============================================ */}
      <div
        className="rounded-2xl overflow-hidden mb-5"
        style={{
          background: '#161b22',
          border: '1px solid #30363d',
        }}
      >
        {/* Gradient accent */}
        <div
          className="h-1"
          style={{
            background: 'linear-gradient(90deg, #00ff88 0%, #8b5cf6 50%, #f59e0b 100%)',
          }}
        />
        <div className="text-center py-10 px-6">
          <p
            className="text-[13px] font-semibold uppercase tracking-[3px]"
            style={{ color: '#00ff88' }}
          >
            Your WikiSurge
          </p>
          <h2
            className="text-4xl sm:text-5xl font-extrabold mt-2"
            style={{
              color: '#e6edf3',
              letterSpacing: '-1px',
            }}
          >
            {period === 'daily' ? 'Daily' : 'Weekly'} Digest
          </h2>
          <div className="mt-4 inline-block">
            <span
              className="text-xs font-mono px-4 py-1.5 rounded-full"
              style={{
                background: '#21262d',
                color: '#8b949e',
              }}
            >
              {dateLabel}
            </span>
          </div>
          <p className="mt-5 text-[15px]" style={{ color: '#8b949e' }}>
            Here's what happened on Wikipedia{' '}
            {period === 'daily' ? 'yesterday' : 'this week'} ⚡
          </p>
        </div>
      </div>

      {/* ============================================ */}
      {/* FUN STATS                                    */}
      {/* ============================================ */}
      <div
        className="rounded-2xl overflow-hidden mb-5"
        style={{
          background: '#161b22',
          border: '1px solid #30363d',
        }}
      >
        <div className="text-center pt-7 pb-2 px-6">
          <p
            className="text-[11px] font-bold uppercase tracking-[3px]"
            style={{ color: '#8b949e' }}
          >
            📊 Fun Stats
          </p>
        </div>

        {/* Hero stat: total edits */}
        <div className="text-center pb-6 px-6">
          <p
            className="text-5xl sm:text-6xl font-extrabold font-mono"
            style={{ color: '#00ff88', letterSpacing: '-2px' }}
          >
            {formatNumber(data.stats.total_edits)}
          </p>
          <p
            className="text-sm font-mono uppercase tracking-widest mt-1"
            style={{ color: '#8b949e' }}
          >
            edits tracked
          </p>
        </div>

        <SectionDivider color="#21262d" />

        {/* Two-column stats */}
        <div className="flex gap-4 px-6 pb-6">
          <StatCard
            value={data.stats.edit_wars}
            label="Edit Wars"
            icon={<Swords className="h-5 w-5" style={{ color: '#ef4444' }} />}
            color="#ef4444"
            bg="rgba(28,16,23,1)"
          />
          <StatCard
            value={
              data.stats.top_languages[0]?.language.toUpperCase() ?? '—'
            }
            label="Top Language"
            icon={<Globe className="h-5 w-5" style={{ color: '#3b82f6' }} />}
            color="#3b82f6"
            bg="rgba(13,26,45,1)"
          />
        </div>

        {/* Language breakdown */}
        {data.stats.top_languages.length > 1 && (
          <>
            <SectionDivider color="#21262d" />
            <div className="px-6 pb-6">
              <p
                className="text-[11px] font-bold uppercase tracking-widest mb-4"
                style={{ color: '#8b949e' }}
              >
                Language Breakdown
              </p>
              {data.stats.top_languages.map((lang) => (
                <LanguageBar
                  key={lang.language}
                  language={lang.language}
                  percentage={lang.percentage}
                />
              ))}
            </div>
          </>
        )}
      </div>

      {/* ============================================ */}
      {/* EDIT WARS                                    */}
      {/* ============================================ */}
      {data.edit_war_highlights.length > 0 && (
        <div
          className="rounded-2xl overflow-hidden mb-5"
          style={{
            background: '#161b22',
            border: '1px solid #30363d',
          }}
        >
          {/* Red accent bar */}
          <div
            className="h-1"
            style={{
              background: 'linear-gradient(90deg, #ef4444 0%, #f59e0b 50%, #ef4444 100%)',
            }}
          />
          <div className="px-6 pt-6 pb-2">
            <p
              className="text-[11px] font-bold uppercase tracking-[3px]"
              style={{ color: '#ef4444' }}
            >
              ⚔️ Most Popular Edit Wars
            </p>
          </div>
          <div className="px-4 pb-5 space-y-3">
            {data.edit_war_highlights.map((item) => (
              <EditWarCard key={item.title} item={item} />
            ))}
          </div>
        </div>
      )}

      {/* ============================================ */}
      {/* TRENDING                                     */}
      {/* ============================================ */}
      {data.trending_highlights.length > 0 && (
        <div
          className="rounded-2xl overflow-hidden mb-5"
          style={{
            background: '#161b22',
            border: '1px solid #30363d',
          }}
        >
          {/* Purple accent bar */}
          <div
            className="h-1"
            style={{
              background: 'linear-gradient(90deg, #8b5cf6 0%, #00ff88 50%, #8b5cf6 100%)',
            }}
          />
          <div className="px-6 pt-6 pb-2">
            <p
              className="text-[11px] font-bold uppercase tracking-[3px]"
              style={{ color: '#8b5cf6' }}
            >
              📈 Trending Pages
            </p>
          </div>
          <div className="px-4 pb-5 space-y-3">
            {data.trending_highlights.map((item) => (
              <TrendingCard key={item.title} item={item} />
            ))}
          </div>
        </div>
      )}

      {/* ============================================ */}
      {/* YOUR WATCHLIST                               */}
      {/* ============================================ */}
      {data.watchlist_events.length > 0 && (
        <div
          className="rounded-2xl overflow-hidden mb-5"
          style={{
            background: '#161b22',
            border: '1px solid #30363d',
          }}
        >
          {/* Green accent bar */}
          <div
            className="h-1"
            style={{
              background: 'linear-gradient(90deg, #00ff88 0%, #22c55e 100%)',
            }}
          />
          <div className="px-6 pt-6 pb-2">
            <div className="flex items-center gap-2">
              <Bookmark className="h-4 w-4" style={{ color: '#00ff88' }} />
              <p
                className="text-[11px] font-bold uppercase tracking-[3px]"
                style={{ color: '#00ff88' }}
              >
                Your Watchlist
              </p>
            </div>
          </div>

          {/* Notable events */}
          {notableWatchlist.length > 0 && (
            <div className="px-4 pb-2">
              <p
                className="text-[10px] font-mono uppercase tracking-widest px-2 mb-2"
                style={{ color: '#f59e0b' }}
              >
                🔥 Notable Activity
              </p>
              <div className="space-y-2">
                {notableWatchlist.map((item) => (
                  <WatchlistItem key={item.title} item={item} />
                ))}
              </div>
            </div>
          )}

          {/* Quiet events */}
          {quietWatchlist.length > 0 && (
            <div className="px-4 pb-5 mt-3">
              <p
                className="text-[10px] font-mono uppercase tracking-widest px-2 mb-2"
                style={{ color: '#8b949e' }}
              >
                😴 All Quiet
              </p>
              <div className="space-y-2">
                {quietWatchlist.map((item) => (
                  <WatchlistItem key={item.title} item={item} />
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* ============================================ */}
      {/* FOOTER                                       */}
      {/* ============================================ */}
      <div
        className="rounded-2xl overflow-hidden mb-8 text-center py-8 px-6"
        style={{
          background: '#161b22',
          border: '1px solid #30363d',
        }}
      >
        <p className="text-[13px] font-mono" style={{ color: '#8b949e' }}>
          This is a <strong style={{ color: '#00ff88' }}>preview</strong> of how your{' '}
          {period} digest email would look.
        </p>
        <p className="text-[11px] font-mono mt-2" style={{ color: '#6b7280' }}>
          Configure your digest preferences in{' '}
          <span style={{ color: '#00ff88' }}>Settings → Digest Email Preferences</span>
        </p>
        <div className="flex items-center justify-center gap-2 mt-4">
          <BarChart3 className="h-4 w-4" style={{ color: '#00ff88' }} />
          <span className="text-[11px] font-mono font-bold" style={{ color: '#00ff88' }}>
            WIKISURGE
          </span>
        </div>
      </div>
    </div>
  );
}
