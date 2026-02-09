import { useState, memo } from 'react';
import { Globe } from 'lucide-react';
import { useAppStore } from '../../store/appStore';

// ── Colors ────────────────────────────────────────────────────────
const LANG_COLORS = [
  '#00ff88', '#00ddff', '#ffaa00', '#ff4444', '#88ff00',
  '#ff44ff', '#00ffdd', '#ff8800', '#44ffaa', '#8888ff',
];

// ── Main component ───────────────────────────────────────────────
export const LanguageDistributionChart = memo(function LanguageDistributionChart() {
  // Get stats from global store (shared with StatsOverview)
  const stats = useAppStore((s) => s.stats);
  const [error] = useState<string | null>(null);

  const languages = stats?.top_languages?.slice(0, 10) ?? [];
  const maxCount = Math.max(...languages.map((l) => l.count), 1);
  const hasDistribution =
    stats?.edits_by_type && (stats.edits_by_type.human > 0 || stats.edits_by_type.bot > 0);

  return (
    <div className="card space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2">
        <Globe className="h-4 w-4" style={{ color: '#00ff88' }} />
        <h3 className="text-xs font-mono font-semibold tracking-wide" style={{ color: '#00ff88' }}>
          LANGUAGE DISTRIBUTION
        </h3>
      </div>

      {error ? (
        <div className="text-sm p-4 font-mono" style={{ color: '#ff4444' }}>Error: {error}</div>
      ) : !stats ? (
        <div className="h-48 flex items-center justify-center text-sm font-mono" style={{ color: 'rgba(0,255,136,0.3)' }}>
          Loading language data…
        </div>
      ) : languages.length === 0 ? (
        <div className="h-48 flex items-center justify-center text-sm font-mono" style={{ color: 'rgba(0,255,136,0.3)' }}>
          No language data available yet
        </div>
      ) : (
        <div className="space-y-2">
          {languages.map((lang, idx) => (
            <div key={lang.language} className="flex items-center gap-3">
              <span className="text-[10px] font-mono font-medium w-8 text-right" style={{ color: 'rgba(0,255,136,0.6)' }}>
                {lang.language}
              </span>
              <div className="flex-1 h-5 rounded overflow-hidden" style={{ background: 'rgba(0,255,136,0.06)' }}>
                <div
                  className="h-full rounded transition-all duration-500"
                  style={{
                    width: `${(lang.count / maxCount) * 100}%`,
                    backgroundColor: LANG_COLORS[idx % LANG_COLORS.length],
                    opacity: 0.7,
                    boxShadow: `0 0 8px ${LANG_COLORS[idx % LANG_COLORS.length]}40`,
                  }}
                />
              </div>
              <span className="text-[10px] font-mono tabular-nums w-24" style={{ color: 'rgba(0,255,136,0.4)' }}>
                {lang.count.toLocaleString()} ({lang.percentage.toFixed(1)}%)
              </span>
            </div>
          ))}
        </div>
      )}

      {/* Human vs Bot distribution */}
      {hasDistribution && (
        <div className="pt-4" style={{ borderTop: '1px solid rgba(0,255,136,0.1)' }}>
          <h4 className="text-[10px] font-mono font-medium uppercase tracking-widest mb-2" style={{ color: 'rgba(0,255,136,0.4)' }}>
            EDIT TYPE DISTRIBUTION
          </h4>
          <div className="flex items-center gap-4">
            <div className="flex-1">
              <div className="flex h-3 rounded overflow-hidden" style={{ background: 'rgba(0,255,136,0.06)' }}>
                <div
                  className="transition-all duration-500"
                  style={{
                    width: `${(stats!.edits_by_type!.human / (stats!.edits_by_type!.human + stats!.edits_by_type!.bot)) * 100}%`,
                    background: '#00ff88',
                    opacity: 0.6,
                  }}
                />
                <div
                  className="transition-all duration-500"
                  style={{
                    width: `${(stats!.edits_by_type!.bot / (stats!.edits_by_type!.human + stats!.edits_by_type!.bot)) * 100}%`,
                    background: 'rgba(0,255,136,0.2)',
                  }}
                />
              </div>
            </div>
            <div className="text-[10px] font-mono space-x-3" style={{ color: 'rgba(0,255,136,0.5)' }}>
              <span>
                <span className="inline-block w-2 h-2 rounded-full mr-1" style={{ background: '#00ff88' }} />
                Human: {stats!.edits_by_type!.human.toLocaleString()}
              </span>
              <span>
                <span className="inline-block w-2 h-2 rounded-full mr-1" style={{ background: 'rgba(0,255,136,0.25)' }} />
                Bot: {stats!.edits_by_type!.bot.toLocaleString()}
              </span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
});
