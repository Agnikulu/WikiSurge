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
function langFlag(lang: string): string {
  const map: Record<string, string> = {
    en: '🇬🇧', es: '🇪🇸', fr: '🇫🇷', de: '🇩🇪',
    ja: '🇯🇵', zh: '🇨🇳', ru: '🇷🇺', pt: '🇧🇷',
    it: '🇮🇹', ar: '🇸🇦', ko: '🇰🇷', nl: '🇳🇱',
    pl: '🇵🇱', sv: '🇸🇪', hi: '🇮🇳', uk: '🇺🇦',
  };
  return map[lang] ?? lang.toUpperCase();
}

/** Rank badge colors for top 3. */
function rankStyle(rank: number): string {
  if (rank === 1)
    return 'bg-gradient-to-br from-yellow-300 to-yellow-500 text-yellow-900 shadow-sm shadow-yellow-200';
  if (rank === 2)
    return 'bg-gradient-to-br from-gray-200 to-gray-400 text-gray-800 shadow-sm shadow-gray-200';
  if (rank === 3)
    return 'bg-gradient-to-br from-orange-300 to-orange-500 text-orange-900 shadow-sm shadow-orange-200';
  return 'bg-gray-100 text-gray-500';
}

export function TrendingCard({ page, rank, previousRank, isNew }: TrendingCardProps) {
  const wikiUrl = buildWikiUrl(page.title);

  // Determine trending direction vs previous position
  let TrendIcon = Minus;
  let trendColor = 'text-gray-400';
  let trendLabel = 'unchanged';
  if (previousRank != null && previousRank !== rank) {
    if (rank < previousRank) {
      TrendIcon = TrendingUp;
      trendColor = 'text-green-500';
      trendLabel = `up ${previousRank - rank}`;
    } else {
      TrendIcon = TrendingDown;
      trendColor = 'text-red-500';
      trendLabel = `down ${rank - previousRank}`;
    }
  } else if (isNew) {
    TrendIcon = TrendingUp;
    trendColor = 'text-green-500';
    trendLabel = 'new entry';
  }

  const isTop3 = rank <= 3;

  return (
    <div
      className={`
        group flex items-center gap-3 p-3 rounded-xl transition-all duration-200
        hover:bg-gray-50 hover:shadow-sm
        ${isNew ? 'animate-slide-up ring-1 ring-green-200' : ''}
        ${isTop3 ? 'py-4' : ''}
      `}
    >
      {/* ── Rank badge ── */}
      <div
        className={`
          flex-shrink-0 flex items-center justify-center rounded-lg font-bold
          ${isTop3 ? 'w-10 h-10 text-base' : 'w-8 h-8 text-sm'}
          ${rankStyle(rank)}
        `}
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
            font-semibold text-gray-900 hover:text-blue-600 hover:underline truncate block
            ${isTop3 ? 'text-base' : 'text-sm'}
          `}
          title={page.title}
        >
          {truncateTitle(page.title, 45)}
        </a>

        {/* Stats row */}
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 mt-0.5 text-xs text-gray-500">
          <span title={`Language: ${page.language}`}>{langFlag(page.language)}</span>
          <span className="hidden sm:inline">·</span>
          <span>{page.edits_1h} edits/hr</span>
          <span className="hidden sm:inline">·</span>
          <span className="hidden sm:inline">{formatTimestamp(page.last_edit)}</span>
        </div>
      </div>

      {/* ── Score + trend ── */}
      <div className="flex-shrink-0 flex flex-col items-end gap-0.5">
        <div className="flex items-center gap-1">
          <TrendIcon className={`h-3.5 w-3.5 ${trendColor}`} aria-label={trendLabel} />
          <span className={`font-bold tabular-nums ${isTop3 ? 'text-base' : 'text-sm'} text-gray-800`}>
            {page.score >= 1000
              ? `${(page.score / 1000).toFixed(1)}k`
              : page.score.toFixed(1)}
          </span>
        </div>
        <span className="text-[10px] text-gray-400">{formatNumber(page.edits_1h)} edits</span>
      </div>
    </div>
  );
}
