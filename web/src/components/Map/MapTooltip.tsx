import { useState, useCallback, useRef, memo } from 'react';
import type { GeoHotspot, GeoWar } from '../../types';

interface MapTooltipProps {
  x: number;
  y: number;
  hotspot?: GeoHotspot;
  war?: GeoWar;
  onViewAnalysis?: (war: GeoWar) => void;
  onPaneMouseEnter?: () => void;
  onPaneMouseLeave?: () => void;
}

export const MapTooltip = memo(function MapTooltip({ x, y, hotspot, war, onViewAnalysis, onPaneMouseEnter, onPaneMouseLeave }: MapTooltipProps) {
  if (!hotspot && !war) return null;

  const severity = war?.severity?.toLowerCase() ?? '';
  const severityColor = severity === 'critical' ? '#ff4444'
    : severity === 'high' ? '#ffaa00'
    : severity === 'medium' ? '#00ff88'
    : '#00ddff';

  // On mobile (<640px), render as bottom sheet instead of cursor-follow
  const isMobile = typeof window !== 'undefined' && window.innerWidth < 640;

  const positionStyle = isMobile
    ? { left: 0, right: 0, bottom: 0, top: 'auto' as const, maxWidth: '100%' }
    : { left: x + 12, top: y - 10, maxWidth: 280 };

  return (
    <div
      className={`fixed z-[100] animate-fade-in`}
      style={positionStyle}
      onMouseEnter={onPaneMouseEnter}
      onMouseLeave={onPaneMouseLeave}
    >
      <div
        className={`${isMobile ? 'rounded-t-xl px-4 py-3' : 'rounded-lg px-3 py-2'} text-xs font-mono shadow-xl pointer-events-auto`}
        style={{
          background: 'rgba(13, 21, 37, 0.95)',
          backdropFilter: 'blur(12px)',
          border: '1px solid rgba(0,255,136,0.2)',
          boxShadow: '0 4px 20px rgba(0,0,0,0.5)',
        }}
      >
        {hotspot && !war && (
          <>
            <div className="flex items-center gap-2 mb-1">
              <span
                className="inline-block w-2 h-2 rounded-full"
                style={{ backgroundColor: hotspot.rank <= 5 ? '#00ffcc' : '#00ff88', boxShadow: `0 0 6px ${hotspot.rank <= 5 ? '#00ffcc' : '#00ff88'}` }}
              />
              <span style={{ color: '#00ff88' }} className="font-bold uppercase tracking-wider text-[10px]">
                #{hotspot.rank} TRENDING
              </span>
            </div>
            <div className="font-bold mb-1 truncate" style={{ color: 'rgba(0,255,136,0.9)' }}>
              {hotspot.page_title}
            </div>
            <div style={{ color: 'rgba(0,255,136,0.6)' }} className="space-y-0.5">
              <div className="flex gap-3">
                <span>Score: {hotspot.score.toFixed(1)}</span>
                {hotspot.edits_1h > 0 && <span>{hotspot.edits_1h} edits/hr</span>}
              </div>
              {hotspot.language && (
                <div className="text-[9px]" style={{ color: 'rgba(0,255,136,0.4)' }}>
                  {hotspot.language}wiki · {hotspot.location_source === 'article' ? '📍 Article' : hotspot.location_source === 'wikidata' ? '🔗 Wikidata' : hotspot.location_source === 'semantic' ? '🧠 Semantic' : '🌐 Wiki region'}
                </div>
              )}
            </div>
          </>
        )}

        {war && (
          <>
            <div className="flex items-center gap-2 mb-1">
              <span
                className="inline-block w-2 h-2 rounded-full"
                style={{ backgroundColor: severityColor, boxShadow: `0 0 6px ${severityColor}` }}
              />
              <span style={{ color: severityColor }} className="font-bold uppercase tracking-wider text-[10px]">
                {war.severity} EDIT WAR
              </span>
            </div>
            <div className="font-bold mb-1 truncate" style={{ color: 'rgba(0,255,136,0.9)' }}>
              {war.page_title}
            </div>
            <div style={{ color: 'rgba(0,255,136,0.5)' }} className="space-y-0.5 text-[10px]">
              <div className="flex gap-3">
                <span>{war.editor_count} editors</span>
                <span>{war.edit_count} edits</span>
                <span>{war.revert_count} reverts</span>
              </div>
              {war.summary_snippet && (
                <div className="mt-1 leading-relaxed" style={{ color: 'rgba(0,255,136,0.4)' }}>
                  {war.summary_snippet}
                </div>
              )}
              <div className="mt-1 flex items-center gap-1" style={{ color: '#00ddff' }}>
                <span className="text-[9px]">
                  {war.location_source === 'article' ? '📍 Article coords' : war.location_source === 'wikidata' ? '🔗 Wikidata location' : war.location_source === 'semantic' ? '🧠 Semantic NER' : '🌐 Wiki region'}
                </span>
              </div>
              {onViewAnalysis && (
                <button
                  onClick={(e) => { e.stopPropagation(); onViewAnalysis(war); }}
                  className="mt-1 text-[10px] underline cursor-pointer pointer-events-auto"
                  style={{ color: '#00ddff' }}
                >
                  View Analysis →
                </button>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
});

// Hook for managing tooltip state with delayed hide so hovering the pane keeps it open.
export function useMapTooltip() {
  const [tooltip, setTooltip] = useState<{
    x: number;
    y: number;
    hotspot?: GeoHotspot;
    war?: GeoWar;
  } | null>(null);

  const hideTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const cancelHide = useCallback(() => {
    if (hideTimer.current) {
      clearTimeout(hideTimer.current);
      hideTimer.current = null;
    }
  }, []);

  const showHotspotTooltip = useCallback((e: React.MouseEvent, hotspot: GeoHotspot) => {
    cancelHide();
    setTooltip({ x: e.clientX, y: e.clientY, hotspot });
  }, [cancelHide]);

  const showWarTooltip = useCallback((e: React.MouseEvent, war: GeoWar) => {
    cancelHide();
    setTooltip({ x: e.clientX, y: e.clientY, war });
  }, [cancelHide]);

  // Delayed hide: gives user time to move mouse from marker to pane
  const hideTooltip = useCallback(() => {
    cancelHide();
    hideTimer.current = setTimeout(() => {
      setTooltip(null);
      hideTimer.current = null;
    }, 150);
  }, [cancelHide]);

  // Called when mouse enters the tooltip pane — cancels pending hide
  const onPaneMouseEnter = useCallback(() => {
    cancelHide();
  }, [cancelHide]);

  // Called when mouse leaves the tooltip pane — start hide timer
  const onPaneMouseLeave = useCallback(() => {
    hideTooltip();
  }, [hideTooltip]);

  return { tooltip, showHotspotTooltip, showWarTooltip, hideTooltip, onPaneMouseEnter, onPaneMouseLeave };
}
