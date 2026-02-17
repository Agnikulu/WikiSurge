import type { TrendingPage } from '../../types';
import { formatTimestamp, formatNumber, truncateTitle, buildWikiUrl } from '../../utils/formatting';
import { TrendingUp, TrendingDown, Minus } from 'lucide-react';

interface TrendingCardProps {
  page: TrendingPage;
  rank: number;
  previousRank?: number | null;
  isNew?: boolean;
}

/** Language code → flag emoji (best-effort). */
function langFlag(lang: string | undefined): string {
  if (!lang) return '🌐';
  const map: Record<string, string> = {
    en: '🇬🇧', es: '🇪🇸', fr: '🇫🇷', de: '🇩🇪',
    ja: '🇯🇵', zh: '🇨🇳', ru: '🇷🇺', pt: '🇧🇷',
    it: '🇮🇹', ar: '🇸🇦', ko: '🇰🇷', nl: '🇳🇱',
    pl: '🇵🇱', sv: '🇸🇪', hi: '🇮🇳', uk: '🇺🇦',
  };
  return map[lang] ?? lang.toUpperCase();
}

export function TrendingCard({ page, rank, previousRank, isNew }: TrendingCardProps) {
  const wikiUrl = buildWikiUrl(page.title, page.server_url);

  // Determine trending direction vs previous position
  let TrendIcon = Minus;
  let trendColor = 'text-gray-400';
  let trendLabel = 'unchanged';
  if (previousRank != null && previousRank !== rank) {
    if (rank < previousRank) {
      TrendIcon = TrendingUp;
      trendColor = '';
      trendLabel = `up ${previousRank - rank}`;
    } else {
      TrendIcon = TrendingDown;
      trendColor = '';
      trendLabel = `down ${rank - previousRank}`;
    }
  } else if (isNew) {
    TrendIcon = TrendingUp;
    trendColor = '';
    trendLabel = 'new entry';
  }

  const isTop3 = rank <= 3;

  return (
    <div
      className={`
        group flex items-center gap-3 p-3 rounded-xl transition-all duration-200
        ${isNew ? 'animate-slide-up' : ''}
        ${isTop3 ? 'py-4' : ''}
      `}
      style={{
        border: isNew ? '1px solid rgba(0,255,136,0.3)' : '1px solid transparent',
      }}
    >
      {/* ── Rank badge ── */}
      <div
        className={`
          flex-shrink-0 flex items-center justify-center rounded-lg font-bold
          ${isTop3 ? 'w-10 h-10 text-base' : 'w-8 h-8 text-sm'}
        `}
        style={{
          background: rank === 1 ? 'rgba(255,170,0,0.2)' : rank === 2 ? 'rgba(0,255,136,0.15)' : rank === 3 ? 'rgba(0,221,255,0.15)' : 'rgba(0,255,136,0.06)',
          color: rank === 1 ? '#ffaa00' : rank === 2 ? '#00ff88' : rank === 3 ? '#00ddff' : 'rgba(0,255,136,0.5)',
          fontFamily: 'monospace',
          border: rank <= 3 ? `1px solid ${rank === 1 ? 'rgba(255,170,0,0.3)' : rank === 2 ? 'rgba(0,255,136,0.3)' : 'rgba(0,221,255,0.3)'}` : '1px solid rgba(0,255,136,0.08)',
        }}
      >
        #{rank}
      </div>

      {/* ── Main info ── */}
      <div className="flex-1 min-w-0">
        <a
          href={wikiUrl}
          target="_blank"
          rel="noopener noreferrer"
          className={`
            font-semibold hover:underline truncate block
            ${isTop3 ? 'text-base' : 'text-sm'}
          `}
          style={{ color: '#00ff88', fontFamily: 'monospace' }}
          title={page.title}
        >
          {truncateTitle(page.title, 45)}
        </a>

        {/* Stats row */}
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 mt-0.5 text-xs" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
          {page.language && <span title={`Language: ${page.language}`}>{langFlag(page.language)}</span>}
          <span className="hidden sm:inline">·</span>
          <span>{page.edits_1h} edits/hr</span>
          <span className="hidden sm:inline">·</span>
          <span className="hidden sm:inline">{formatTimestamp(page.last_edit)}</span>
        </div>
      </div>

      {/* ── Score + trend ── */}
      <div className="flex-shrink-0 flex flex-col items-end gap-0.5">
        <div className="flex items-center gap-1">
          <TrendIcon className={`h-3.5 w-3.5 ${trendColor}`} style={{ color: trendLabel.startsWith('up') || trendLabel === 'new entry' ? '#00ff88' : trendLabel.startsWith('down') ? '#ff4444' : 'rgba(0,255,136,0.3)' }} aria-label={trendLabel} />
          <span className={`font-bold tabular-nums ${isTop3 ? 'text-base' : 'text-sm'}`} style={{ color: '#00ff88', fontFamily: 'monospace' }}>
            {page.score >= 1000
              ? `${(page.score / 1000).toFixed(1)}k`
              : page.score.toFixed(1)}
          </span>
        </div>
        <span className="text-[10px]" style={{ color: 'rgba(0,255,136,0.35)', fontFamily: 'monospace' }}>{formatNumber(page.edits_1h)} edits</span>
      </div>
    </div>
  );
}
