import { memo } from 'react';
import { Zap, Users } from 'lucide-react';
import type { Side } from '../../types';
import { buildWikiUrl } from '../../utils/formatting';

const SIDE_COLORS = [
  { accent: 'rgba(59, 130, 246, 0.8)', bg: 'rgba(59, 130, 246, 0.06)', border: 'rgba(59, 130, 246, 0.2)', gradient: 'from-blue-700 to-cyan-700', tint: 'blue' },
  { accent: 'rgba(239, 68, 68, 0.8)', bg: 'rgba(239, 68, 68, 0.06)', border: 'rgba(239, 68, 68, 0.2)', gradient: 'from-red-700 to-orange-700', tint: 'red' },
  { accent: 'rgba(234, 179, 8, 0.8)', bg: 'rgba(234, 179, 8, 0.06)', border: 'rgba(234, 179, 8, 0.2)', gradient: 'from-yellow-700 to-amber-700', tint: 'yellow' },
  { accent: 'rgba(168, 85, 247, 0.8)', bg: 'rgba(168, 85, 247, 0.06)', border: 'rgba(168, 85, 247, 0.2)', gradient: 'from-purple-700 to-pink-700', tint: 'purple' },
];

interface SidesMatchupProps {
  sides: Side[];
  revertRatio?: number;
  serverUrl?: string;
  compact?: boolean;
}

export const SidesMatchup = memo(function SidesMatchup({
  sides,
  revertRatio = 0,
  serverUrl,
  compact = false,
}: SidesMatchupProps) {
  if (!sides || sides.length === 0) {
    return (
      <div
        className="flex items-center justify-center py-4 rounded-lg"
        style={{
          background: 'rgba(139, 92, 246, 0.03)',
          border: '1px solid rgba(139, 92, 246, 0.1)',
        }}
      >
        <div className="text-center space-y-1">
          <div
            className="animate-shimmer h-2 w-32 rounded mx-auto"
            style={{
              background: 'linear-gradient(90deg, transparent, rgba(139,92,246,0.1), transparent)',
              backgroundSize: '200% 100%',
            }}
          />
          <span
            className="text-[10px] font-mono"
            style={{ color: 'rgba(139,92,246,0.4)' }}
          >
            Analysis pending...
          </span>
        </div>
      </div>
    );
  }

  // Single side → consensus layout
  if (sides.length === 1) {
    return (
      <div
        className="rounded-lg p-3"
        style={{
          background: SIDE_COLORS[0].bg,
          border: `1px solid ${SIDE_COLORS[0].border}`,
        }}
      >
        <div className="flex items-center gap-2 mb-2">
          <Users className="h-3.5 w-3.5" style={{ color: SIDE_COLORS[0].accent }} />
          <span
            className="text-[10px] font-mono font-bold uppercase tracking-wider"
            style={{ color: SIDE_COLORS[0].accent }}
          >
            Consensus View
          </span>
        </div>
        <p className="text-[11px] font-mono mb-2" style={{ color: 'rgba(255,255,255,0.6)' }}>
          {sides[0].position}
        </p>
        <div className="flex flex-wrap gap-1">
          {sides[0].editors.map((ed) => (
            <EditorChip key={ed.user} editor={ed} colors={SIDE_COLORS[0]} serverUrl={serverUrl} compact={compact} />
          ))}
        </div>
      </div>
    );
  }

  // Two+ sides → VS layout
  const leftSide = sides[0];
  const rightSide = sides[1];
  const extraSides = sides.slice(2);
  const vsGlowIntensity = Math.min(1, revertRatio * 2); // 0-1

  return (
    <div className="space-y-3">
      {/* Main VS grid */}
      <div className={`grid ${compact ? 'grid-cols-1 gap-2' : 'grid-cols-[1fr,auto,1fr] gap-3'} items-stretch`}>
        {/* Left side (blue) */}
        <SidePanel side={leftSide} colors={SIDE_COLORS[0]} serverUrl={serverUrl} compact={compact} />

        {/* VS divider */}
        {!compact && (
          <div className="flex flex-col items-center justify-center px-0.5">
            <div
              className="w-10 h-10 rounded-full flex items-center justify-center animate-pulse-border"
              style={{
                background: `rgba(255, 100, 50, ${0.08 + vsGlowIntensity * 0.12})`,
                border: `2px solid rgba(255, 100, 50, ${0.2 + vsGlowIntensity * 0.3})`,
                boxShadow: `0 0 ${10 + vsGlowIntensity * 20}px rgba(255, 100, 50, ${0.1 + vsGlowIntensity * 0.2})`,
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
        )}

        {/* Right side (red) */}
        <SidePanel side={rightSide} colors={SIDE_COLORS[1]} serverUrl={serverUrl} compact={compact} />
      </div>

      {/* Extra sides */}
      {extraSides.map((side, idx) => (
        <SidePanel key={idx + 2} side={side} colors={SIDE_COLORS[(idx + 2) % SIDE_COLORS.length]} serverUrl={serverUrl} compact={compact} />
      ))}
    </div>
  );
});

function SidePanel({
  side,
  colors,
  serverUrl,
  compact,
}: {
  side: Side;
  colors: typeof SIDE_COLORS[number];
  serverUrl?: string;
  compact?: boolean;
}) {
  return (
    <div
      className="rounded-lg overflow-hidden"
      style={{
        background: colors.bg,
        border: `1px solid ${colors.border}`,
      }}
    >
      <div className="px-3 py-2" style={{ borderBottom: `1px solid ${colors.border}` }}>
        <p
          className="text-[11px] font-medium leading-snug font-mono"
          style={{ color: colors.accent }}
        >
          {side.position}
        </p>
      </div>
      <div className={`p-2 ${compact ? 'space-y-1' : 'space-y-1.5'}`}>
        {side.editors.map((editor) => (
          <EditorChip key={editor.user} editor={editor} colors={colors} serverUrl={serverUrl} compact={compact} />
        ))}
      </div>
    </div>
  );
}

function EditorChip({
  editor,
  colors,
  serverUrl,
  compact,
}: {
  editor: { user: string; edit_count: number; role: string };
  colors: typeof SIDE_COLORS[number];
  serverUrl?: string;
  compact?: boolean;
}) {
  return (
    <a
      href={buildWikiUrl(`User:${editor.user}`, serverUrl)}
      target="_blank"
      rel="noopener noreferrer"
      className="flex items-center gap-2 px-2 py-1.5 rounded-md transition-colors hover:bg-white/5"
    >
      <span
        className={`${compact ? 'w-5 h-5 text-[9px]' : 'w-7 h-7 text-[11px]'} rounded-full bg-gradient-to-br ${colors.gradient} flex items-center justify-center text-white font-bold flex-shrink-0`}
      >
        {editor.user.charAt(0).toUpperCase()}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className={`${compact ? 'text-[10px]' : 'text-xs'} font-semibold truncate`} style={{ color: 'rgba(255,255,255,0.85)' }}>
            {editor.user}
          </span>
          <span
            className="text-[9px] px-1 py-0 rounded-full flex-shrink-0"
            style={{
              background: `${colors.accent}15`,
              color: colors.accent,
              border: `1px solid ${colors.border}`,
            }}
          >
            {editor.edit_count}e
          </span>
        </div>
        {!compact && (
          <p className="text-[10px] leading-tight mt-0.5 truncate font-mono" style={{ color: 'rgba(255,255,255,0.4)' }}>
            {editor.role}
          </p>
        )}
      </div>
    </a>
  );
}

export default SidesMatchup;
