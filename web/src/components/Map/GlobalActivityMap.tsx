import { useState, useEffect, useCallback, memo, useMemo } from 'react';
import {
  ComposableMap,
  Geographies,
  Geography,
  Graticule,
  Marker,
} from 'react-simple-maps';
import { getGeoActivity } from '../../utils/api';
import type { GeoHotspot, GeoWar, GeoActivityResponse } from '../../types';
import { MapTooltip, useMapTooltip } from './MapTooltip';
import { MapSkeleton } from './MapSkeleton';

const GEO_URL = 'https://cdn.jsdelivr.net/npm/world-atlas@2/countries-110m.json';

const SEVERITY_COLORS: Record<string, string> = {
  critical: '#ff4444',
  high: '#ffaa00',
  medium: '#00ff88',
  low: '#00ddff',
};

/** Responsive height: desktop → height prop, tablet → 75%, mobile → 62% */
function useResponsiveHeight(desktopHeight: number): number {
  const [h, setH] = useState(desktopHeight);
  useEffect(() => {
    const update = () => {
      const w = window.innerWidth;
      if (w < 640) setH(Math.round(desktopHeight * 0.62));
      else if (w < 1024) setH(Math.round(desktopHeight * 0.75));
      else setH(desktopHeight);
    };
    update();
    window.addEventListener('resize', update);
    return () => window.removeEventListener('resize', update);
  }, [desktopHeight]);
  return h;
}

interface GlobalActivityMapProps {
  height?: number;
  onWarClick?: (war: GeoWar) => void;
}

export const GlobalActivityMap = memo(function GlobalActivityMap({
  height = 400,
  onWarClick,
}: GlobalActivityMapProps) {
  const responsiveHeight = useResponsiveHeight(height);
  const [data, setData] = useState<GeoActivityResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const { tooltip, showHotspotTooltip, showWarTooltip, hideTooltip } = useMapTooltip();

  const fetchData = useCallback(async () => {
    try {
      const resp = await getGeoActivity();
      setData(resp);
      setError(false);
    } catch {
      setError(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
    const interval = setInterval(fetchData, 15000); // Poll every 15s
    return () => clearInterval(interval);
  }, [fetchData]);

  // Compute max score for hotspot scaling
  const maxScore = useMemo(() => {
    if (!data?.hotspots?.length) return 1;
    return Math.max(1, ...data.hotspots.map((h) => h.score));
  }, [data]);

  if (loading) return <MapSkeleton height={responsiveHeight} />;
  if (error || !data) {
    return (
      <div
        className="card flex items-center justify-center font-mono text-xs"
        style={{ height: responsiveHeight, color: 'rgba(0,255,136,0.3)' }}
      >
        Map data unavailable
      </div>
    );
  }

  return (
    <div
      className="card relative overflow-hidden animate-map-entrance"
      style={{ padding: 0 }}
    >
      {/* Ocean gradient background */}
      <div
        className="absolute inset-0"
        style={{
          background: 'radial-gradient(ellipse 120% 80% at 50% 40%, #0c1a2e 0%, #070d1a 50%, #050a14 100%)',
        }}
      />

      {/* Subtle grid pattern overlay */}
      <div
        className="absolute inset-0 opacity-[0.03]"
        style={{
          backgroundImage: 'linear-gradient(rgba(0,255,136,1) 1px, transparent 1px), linear-gradient(90deg, rgba(0,255,136,1) 1px, transparent 1px)',
          backgroundSize: '60px 60px',
        }}
      />

      {/* Header */}
      <div
        className="absolute top-3 left-4 z-10 flex items-center gap-2"
        style={{ pointerEvents: 'none' }}
      >
        <span
          className="inline-block w-2 h-2 rounded-full animate-pulse"
          style={{ backgroundColor: '#00ff88', boxShadow: '0 0 8px #00ff88' }}
        />
        <span
          className="text-[10px] font-mono font-bold tracking-widest uppercase"
          style={{ color: 'rgba(0,255,136,0.6)' }}
        >
          GLOBAL ACTIVITY
        </span>
        {data.wars.filter((w) => w.active).length > 0 && (
          <span
            className="text-[10px] font-mono px-1.5 py-0.5 rounded"
            style={{
              background: 'rgba(255,68,68,0.15)',
              color: '#ff4444',
              border: '1px solid rgba(255,68,68,0.3)',
            }}
          >
            {data.wars.filter((w) => w.active).length} WAR{data.wars.filter((w) => w.active).length > 1 ? 'S' : ''}
          </span>
        )}
      </div>

      {/* Legend */}
      <div
        className="absolute bottom-3 left-4 z-10 flex items-center gap-4 text-[9px] font-mono"
        style={{ color: 'rgba(0,255,136,0.5)', pointerEvents: 'none' }}
      >
        <span className="flex items-center gap-1.5">
          <span
            className="inline-block w-2.5 h-2.5 rounded-full"
            style={{ backgroundColor: '#00ff88', opacity: 0.7, boxShadow: '0 0 4px rgba(0,255,136,0.5)' }}
          />
          Trending
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="inline-block w-3 h-3 rounded-full"
            style={{ border: '2px solid #ffaa00', opacity: 0.8, boxShadow: '0 0 4px rgba(255,170,0,0.4)' }}
          />
          Edit War
        </span>
      </div>

      {/* Map */}
      <div className="relative z-[1]">
        <ComposableMap
          projection="geoMercator"
          projectionConfig={{ scale: 140, center: [10, 20] }}
          style={{ width: '100%', height: responsiveHeight }}
        >
          <defs>
            <linearGradient id="countryGrad" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#1a2744" />
              <stop offset="100%" stopColor="#0f1b30" />
            </linearGradient>
            <filter id="countryGlow">
              <feGaussianBlur stdDeviation="2" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>

          {/* Graticule — subtle lat/lng grid */}
          <Graticule stroke="rgba(0,255,136,0.04)" strokeWidth={0.5} />

          {/* Countries */}
          <Geographies geography={GEO_URL}>
            {({ geographies }) =>
              geographies.map((geo) => (
                <Geography
                  key={geo.rsmKey}
                  geography={geo}
                  fill="url(#countryGrad)"
                  stroke="rgba(0,255,136,0.15)"
                  strokeWidth={0.6}
                  style={{
                    default: { outline: 'none' },
                    hover: { outline: 'none', fill: '#1e3050', stroke: 'rgba(0,255,136,0.3)', strokeWidth: 0.8 },
                    pressed: { outline: 'none' },
                  }}
                />
              ))
            }
          </Geographies>

          {/* Trending hotspot markers */}
          {(data.hotspots ?? []).map((hotspot, i) => (
            <HotspotMarker
              key={`hotspot-${hotspot.page_title}`}
              hotspot={hotspot}
              maxScore={maxScore}
              delayIndex={i}
              onMouseEnter={showHotspotTooltip}
              onMouseLeave={hideTooltip}
            />
          ))}

          {/* Edit war markers */}
          {data.wars.map((war) => (
            <WarMarker
              key={`war-${war.page_title}`}
              war={war}
              onMouseEnter={showWarTooltip}
              onMouseLeave={hideTooltip}
              onClick={onWarClick}
            />
          ))}
        </ComposableMap>
      </div>

      {/* Edge vignette */}
      <div
        className="absolute inset-0 z-[2] pointer-events-none"
        style={{
          boxShadow: 'inset 0 0 60px 20px rgba(5,10,20,0.7), inset 0 0 120px 60px rgba(5,10,20,0.3)',
        }}
      />

      {/* Tooltip */}
      {tooltip && (
        <MapTooltip
          x={tooltip.x}
          y={tooltip.y}
          hotspot={tooltip.hotspot}
          war={tooltip.war}
          onViewAnalysis={onWarClick}
        />
      )}
    </div>
  );
});

// --- Hotspot Marker (trending page, green pulse) ---
interface HotspotMarkerProps {
  hotspot: GeoHotspot;
  maxScore: number;
  delayIndex: number;
  onMouseEnter: (e: React.MouseEvent, hotspot: GeoHotspot) => void;
  onMouseLeave: () => void;
}

const HotspotMarker = memo(function HotspotMarker({
  hotspot,
  maxScore,
  delayIndex,
  onMouseEnter,
  onMouseLeave,
}: HotspotMarkerProps) {
  const normalized = Math.min(1, hotspot.score / maxScore);
  const baseRadius = 2.5 + normalized * 10; // 2.5-12.5px
  const opacity = 0.3 + normalized * 0.5;
  const delay = (delayIndex * 0.3) % 2;
  // Top-ranked pages get brighter/different tint
  const isTop5 = hotspot.rank <= 5;
  const color = isTop5 ? '#00ffcc' : '#00ff88';

  return (
    <Marker coordinates={[hotspot.lng, hotspot.lat]}>
      {/* Outer glow pulse */}
      <circle
        r={baseRadius + 4}
        fill="none"
        stroke={color}
        strokeWidth={1}
        opacity={opacity * 0.3}
        className="animate-activity-pulse"
        style={{ animationDelay: `${delay}s` }}
      />
      {/* Main circle */}
      <circle
        r={baseRadius}
        fill={color}
        opacity={opacity}
        className="animate-activity-pulse"
        style={{
          animationDelay: `${delay}s`,
          filter: `drop-shadow(0 0 ${3 + normalized * 6}px ${color}80)`,
          cursor: 'pointer',
        }}
        onMouseEnter={(e) => onMouseEnter(e as unknown as React.MouseEvent, hotspot)}
        onMouseLeave={onMouseLeave}
      />
      {/* Center dot */}
      <circle r={1.5} fill={color} opacity={0.9} />
    </Marker>
  );
});

// --- War Marker (alert ring) ---
interface WarMarkerProps {
  war: GeoWar;
  onMouseEnter: (e: React.MouseEvent, war: GeoWar) => void;
  onMouseLeave: () => void;
  onClick?: (war: GeoWar) => void;
}

const WarMarker = memo(function WarMarker({
  war,
  onMouseEnter,
  onMouseLeave,
  onClick,
}: WarMarkerProps) {
  const color = SEVERITY_COLORS[war.severity?.toLowerCase()] ?? '#ffaa00';
  const isCritical = war.severity?.toLowerCase() === 'critical';
  const radius = isCritical ? 10 : 8;

  return (
    <Marker coordinates={[war.lng, war.lat]}>
      {/* Expanding ring pulse */}
      <circle
        r={radius + 8}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        opacity={0.4}
        className="animate-war-pulse"
      />
      {/* Outer ring */}
      <circle
        r={radius + 3}
        fill="none"
        stroke={color}
        strokeWidth={2}
        opacity={0.6}
        className="animate-war-pulse"
        style={{
          filter: `drop-shadow(0 0 8px ${color})`,
        }}
      />
      {/* Inner solid ring */}
      <circle
        r={radius}
        fill="none"
        stroke={color}
        strokeWidth={2.5}
        opacity={0.9}
        style={{
          cursor: 'pointer',
          filter: `drop-shadow(0 0 12px ${color})`,
        }}
        onMouseEnter={(e) => onMouseEnter(e as unknown as React.MouseEvent, war)}
        onMouseLeave={onMouseLeave}
        onClick={() => onClick?.(war)}
      />
      {/* Center dot */}
      <circle r={2} fill={color} opacity={1} />
      {/* Label */}
      <text
        textAnchor="middle"
        y={-radius - 8}
        style={{
          fontFamily: 'JetBrains Mono, monospace',
          fontSize: 8,
          fill: color,
          opacity: 0.8,
          pointerEvents: 'none',
        }}
      >
        {war.page_title.length > 20 ? war.page_title.slice(0, 18) + '…' : war.page_title}
      </text>
    </Marker>
  );
});

export default GlobalActivityMap;
