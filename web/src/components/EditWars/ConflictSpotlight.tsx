import { useState, useEffect, useRef, memo } from 'react';
import {
  Brain,
  ChevronLeft,
  ChevronRight,
  Clock,
  ExternalLink,
  FileEdit,
  Lightbulb,
  Pause,
  Play,
  RotateCcw,
  Sparkles,
  Swords,
  Target,
  Users,
} from 'lucide-react';
import type { GeoWar, EditWarAnalysis } from '../../types';
import { SidesMatchup } from './SidesMatchup';
import { SeverityBadge } from '../Alerts/SeverityBadge';
import { buildWikiUrl, formatTimestamp } from '../../utils/formatting';

interface ConflictSpotlightProps {
  wars: GeoWar[];
  onViewDetails?: (war: GeoWar) => void;
}

const ROTATE_INTERVAL = 10000; // 10s

export const ConflictSpotlight = memo(function ConflictSpotlight({
  wars,
  onViewDetails,
}: ConflictSpotlightProps) {
  const [activeIndex, setActiveIndex] = useState(0);
  const [paused, setPaused] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Filter to high/critical for carousel, but always show at least one
  const spotlightWars = wars.length > 0 ? wars : [];
  const showCarousel = spotlightWars.length > 1;

  // Auto-rotate
  useEffect(() => {
    if (!showCarousel || paused) {
      if (intervalRef.current) clearInterval(intervalRef.current);
      return;
    }
    intervalRef.current = setInterval(() => {
      setActiveIndex((i) => (i + 1) % spotlightWars.length);
    }, ROTATE_INTERVAL);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [showCarousel, paused, spotlightWars.length]);

  // Reset index if wars change
  useEffect(() => {
    setActiveIndex(0);
  }, [wars.length]);

  if (spotlightWars.length === 0) return null;

  const war = spotlightWars[Math.min(activeIndex, spotlightWars.length - 1)];
  const analysis = war.analysis as EditWarAnalysis | undefined;

  return (
    <div id="conflict-spotlight" className="card animate-spotlight-crossfade" style={{ padding: 0 }}>
      {/* Header bar */}
      <div
        className="flex items-center justify-between px-4 py-2"
        style={{ borderBottom: '1px solid rgba(0,255,136,0.08)' }}
      >
        <div className="flex items-center gap-2">
          <Swords className="h-4 w-4" style={{ color: war.active ? '#ff4444' : 'rgba(0,255,136,0.4)' }} />
          <span className="text-[11px] font-mono font-bold uppercase tracking-widest" style={{ color: war.active ? '#ff4444' : 'rgba(0,255,136,0.4)' }}>
            {war.active ? 'CONFLICT SPOTLIGHT' : 'LATEST RESOLVED CONFLICT'}
          </span>
          {!war.active && war.start_time && (
            <span className="text-[10px] font-mono" style={{ color: 'rgba(0,255,136,0.25)' }}>
              · ended {formatTimestamp(war.start_time)}
            </span>
          )}
        </div>

        {/* Carousel controls */}
        {showCarousel && (
          <div className="flex items-center gap-1">
            <button
              onClick={() => setActiveIndex((i) => (i - 1 + spotlightWars.length) % spotlightWars.length)}
              className="p-1 rounded hover:bg-white/5 transition-colors"
              style={{ color: 'rgba(0,255,136,0.4)' }}
              aria-label="Previous war"
            >
              <ChevronLeft className="h-3.5 w-3.5" />
            </button>
            <span className="text-[10px] font-mono" style={{ color: 'rgba(0,255,136,0.3)' }}>
              {activeIndex + 1}/{spotlightWars.length}
            </span>
            <button
              onClick={() => setActiveIndex((i) => (i + 1) % spotlightWars.length)}
              className="p-1 rounded hover:bg-white/5 transition-colors"
              style={{ color: 'rgba(0,255,136,0.4)' }}
              aria-label="Next war"
            >
              <ChevronRight className="h-3.5 w-3.5" />
            </button>
            <button
              onClick={() => setPaused(!paused)}
              className="p-1 rounded hover:bg-white/5 transition-colors ml-1"
              style={{ color: 'rgba(0,255,136,0.4)' }}
              aria-label={paused ? 'Resume auto-rotate' : 'Pause auto-rotate'}
            >
              {paused ? <Play className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
            </button>
          </div>
        )}
      </div>

      {/* Main 3-panel content */}
      <div
        key={war.page_title}
        className="grid grid-cols-1 lg:grid-cols-3 gap-4 p-4 animate-spotlight-crossfade"
        style={{ opacity: war.active ? 1 : 0.7 }}
      >
        {/* LEFT: Title + severity + stats */}
        <div className="space-y-3">
          {/* Title */}
          <div>
            <a
              href={buildWikiUrl(war.page_title, war.server_url)}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm font-bold font-mono hover:underline leading-tight block"
              style={{ color: '#00ff88' }}
            >
              {war.page_title}
            </a>
            <div className="flex items-center gap-2 mt-1.5">
              <SeverityBadge severity={war.severity} />
              {war.active && (
                <span className="flex items-center gap-1 text-[10px] font-mono" style={{ color: 'rgba(255,68,68,0.7)' }}>
                  <span className="inline-block w-1.5 h-1.5 rounded-full bg-red-500 animate-pulse" />
                  Active
                </span>
              )}
              {war.location_source === 'article' && (
                <span className="text-[9px] font-mono px-1.5 py-0.5 rounded" style={{ background: 'rgba(0,221,255,0.08)', color: 'rgba(0,221,255,0.5)', border: '1px solid rgba(0,221,255,0.15)' }}>
                  📍 Geolocated
                </span>
              )}
            </div>
          </div>

          {/* Live duration */}
          {war.start_time && (
            <div className="flex items-center gap-1.5 text-[10px] font-mono" style={{ color: 'rgba(0,255,136,0.4)' }}>
              <Clock className="h-3 w-3" />
              <LiveDuration startTime={war.start_time} active={war.active} />
            </div>
          )}

          {/* Stats grid */}
          <div className="grid grid-cols-3 gap-2">
            <StatBox icon={<Users className="h-3 w-3" />} value={war.editor_count} label="Editors" />
            <StatBox icon={<FileEdit className="h-3 w-3" />} value={war.edit_count} label="Edits" />
            <StatBox icon={<RotateCcw className="h-3 w-3" />} value={war.revert_count} label="Reverts" />
          </div>

          {/* Actions */}
          <div className="flex gap-2 pt-1">
            <a
              href={buildWikiUrl(war.page_title, war.server_url)}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 px-2 py-1 text-[10px] font-mono rounded transition-colors"
              style={{ background: 'rgba(0,255,136,0.08)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.15)' }}
            >
              <ExternalLink className="h-2.5 w-2.5" />
              VIEW
            </a>
            {onViewDetails && (
              <button
                onClick={() => onViewDetails(war)}
                className="inline-flex items-center gap-1 px-2 py-1 text-[10px] font-mono rounded transition-colors"
                style={{ background: 'rgba(0,221,255,0.08)', color: '#00ddff', border: '1px solid rgba(0,221,255,0.15)' }}
              >
                <Target className="h-2.5 w-2.5" />
                DETAILS
              </button>
            )}
          </div>
        </div>

        {/* CENTER: LLM Summary */}
        <div className="space-y-3">
          {analysis ? (
            <>
              {/* AI badge */}
              <div className="flex items-center gap-1.5">
                <Sparkles className="h-3 w-3" style={{ color: '#a78bfa' }} />
                <span className="text-[10px] font-mono font-bold uppercase tracking-wider" style={{ color: '#c4b5fd' }}>
                  AI Analysis
                </span>
              </div>

              {/* Summary with typewriter */}
              <div
                className="rounded-lg px-3 py-2.5 relative overflow-hidden animate-typewriter"
                style={{
                  background: 'linear-gradient(135deg, rgba(139,92,246,0.06), rgba(59,130,246,0.03))',
                  border: '1px solid rgba(139,92,246,0.12)',
                }}
              >
                <div
                  className="absolute top-0 left-0 right-0 h-[2px]"
                  style={{ background: 'linear-gradient(90deg, #a78bfa, #3b82f6, #00ff88)' }}
                />
                <p className="text-[11px] leading-[1.6] font-mono" style={{ color: 'rgba(255,255,255,0.75)' }}>
                  {analysis.summary}
                </p>
              </div>

              {/* Content area tag */}
              {analysis.content_area && (
                <div className="flex items-center gap-1.5">
                  <Target className="h-3 w-3" style={{ color: 'rgba(0,221,255,0.5)' }} />
                  <span className="text-[10px] font-mono" style={{ color: 'rgba(0,221,255,0.6)' }}>
                    {analysis.content_area}
                  </span>
                </div>
              )}

              {/* Recommendation */}
              {analysis.recommendation && (
                <div
                  className="rounded px-3 py-2"
                  style={{
                    background: 'rgba(234,179,8,0.05)',
                    border: '1px solid rgba(234,179,8,0.15)',
                  }}
                >
                  <div className="flex items-start gap-1.5">
                    <Lightbulb className="h-3 w-3 mt-0.5 flex-shrink-0" style={{ color: 'rgba(234,179,8,0.6)' }} />
                    <p className="text-[10px] leading-relaxed font-mono" style={{ color: 'rgba(234,179,8,0.5)' }}>
                      {analysis.recommendation}
                    </p>
                  </div>
                </div>
              )}
            </>
          ) : (
            <div className="flex flex-col items-center justify-center h-full py-6 space-y-2">
              <Brain className="h-5 w-5" style={{ color: 'rgba(139,92,246,0.3)' }} />
              <span className="text-[10px] font-mono" style={{ color: 'rgba(139,92,246,0.4)' }}>
                Analysis pending...
              </span>
              <div
                className="w-24 h-1 rounded animate-shimmer"
                style={{
                  background: 'linear-gradient(90deg, transparent, rgba(139,92,246,0.15), transparent)',
                  backgroundSize: '200% 100%',
                }}
              />
            </div>
          )}
        </div>

        {/* RIGHT: VS Matchup */}
        <div>
          <SidesMatchup
            sides={analysis?.sides ?? []}
            revertRatio={war.edit_count > 0 ? war.revert_count / war.edit_count : 0}
            serverUrl={war.server_url}
          />
        </div>
      </div>

      {/* Carousel dots */}
      {showCarousel && (
        <div className="flex justify-center gap-1.5 pb-3">
          {spotlightWars.map((_, i) => (
            <button
              key={i}
              onClick={() => setActiveIndex(i)}
              className="rounded-full transition-all"
              style={{
                width: i === activeIndex ? 16 : 6,
                height: 6,
                background: i === activeIndex ? '#00ff88' : 'rgba(0,255,136,0.15)',
              }}
              aria-label={`Show war ${i + 1}`}
            />
          ))}
        </div>
      )}
    </div>
  );
});

// --- Sub-components ---

function StatBox({ icon, value, label }: { icon: React.ReactNode; value: number; label: string }) {
  return (
    <div
      className="rounded-lg px-2 py-1.5 text-center"
      style={{ background: 'rgba(0,255,136,0.04)', border: '1px solid rgba(0,255,136,0.08)' }}
    >
      <div className="flex items-center justify-center gap-1 mb-0.5" style={{ color: 'rgba(0,255,136,0.3)' }}>
        {icon}
      </div>
      <div className="text-sm font-bold font-mono" style={{ color: '#00ff88' }}>
        {value}
      </div>
      <div className="text-[9px] font-mono uppercase tracking-wider" style={{ color: 'rgba(0,255,136,0.3)' }}>
        {label}
      </div>
    </div>
  );
}

function LiveDuration({ startTime, active }: { startTime: string; active: boolean }) {
  const [display, setDisplay] = useState('');

  useEffect(() => {
    const update = () => {
      try {
        const start = new Date(startTime).getTime();
        const now = Date.now();
        const diff = now - start;
        if (diff <= 0) {
          setDisplay('just now');
          return;
        }
        const mins = Math.floor(diff / 60_000);
        if (mins < 1) {
          setDisplay('< 1 min');
          return;
        }
        if (mins < 60) {
          setDisplay(`${mins}m`);
          return;
        }
        const hours = Math.floor(mins / 60);
        if (hours < 24) {
          setDisplay(`${hours}h ${mins % 60}m`);
          return;
        }
        const days = Math.floor(hours / 24);
        setDisplay(`${days}d ${hours % 24}h`);
      } catch {
        setDisplay('unknown');
      }
    };
    update();
    if (active) {
      const interval = setInterval(update, 10_000);
      return () => clearInterval(interval);
    }
  }, [startTime, active]);

  return <span>{active ? `Active · ${display}` : display}</span>;
}

export default ConflictSpotlight;
