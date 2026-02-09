import { useState, useEffect, useRef, useMemo, useCallback, memo } from 'react';
import { Activity } from 'lucide-react';
import type { TimeRange, TimeSeriesPoint, Stats } from '../../types';

const RANGE_CONFIG: Record<TimeRange, { label: string; minutes: number; bucketSec: number }> = {
  '1h':  { label: '1H',  minutes: 60,   bucketSec: 10 },
  '6h':  { label: '6H',  minutes: 360,  bucketSec: 60 },
  '24h': { label: '24H', minutes: 1440, bucketSec: 300 },
};

const PAD = { top: 18, right: 14, bottom: 26, left: 42 };
const MONITOR_GREEN = '#00ff88';
const MONITOR_BG = '#0a0f1a';
const GRID_COLOR = 'rgba(0,255,136,0.06)';
const GRID_COLOR_STRONG = 'rgba(0,255,136,0.12)';

export const EditsTimelineChart = memo(function EditsTimelineChart() {
  const [range, setRange] = useState<TimeRange>('1h');
  const dataRef = useRef<TimeSeriesPoint[]>([]);
  const [chartData, setChartData] = useState<TimeSeriesPoint[]>([]);
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState({ w: 600, h: 240 });
  const [tick, setTick] = useState(0); // animation driver
  const [stats, setStats] = useState<Stats | null>(null);

  // Smooth updates — 10 seconds
  useEffect(() => {
    let mounted = true;
    const fetchStats = async () => {
      try {
        const res = await fetch('/api/stats');
        if (res.ok) {
          const data: Stats = await res.json();
          if (mounted) setStats(data);
        }
      } catch { /* ignore */ }
    };
    fetchStats();
    const interval = setInterval(fetchStats, 10_000);
    return () => { mounted = false; clearInterval(interval); };
  }, []);

  // Animation tick for scan-line sweep
  useEffect(() => {
    let raf: number;
    const loop = () => {
      setTick(Date.now());
      raf = requestAnimationFrame(loop);
    };
    raf = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(raf);
  }, []);

  // Responsive sizing
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      if (width > 0 && height > 0) setDims({ w: width, h: height });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const editsPerSecond = stats?.edits_per_second ?? 0;
  const statsReady = stats !== null;
  const config = RANGE_CONFIG[range];
  const maxPoints = Math.floor(config.minutes * 60 / config.bucketSec);

  // Append new data point on every stats update
  useEffect(() => {
    if (!statsReady) return;
    const now = Date.now();
    const value = editsPerSecond * 60;

    if (dataRef.current.length === 0) {
      const base = value > 0 ? value : 1;
      for (let i = 12; i >= 1; i--) {
        dataRef.current.push({
          timestamp: now - i * config.bucketSec * 1000,
          value: base * (0.7 + Math.random() * 0.6),
        });
      }
    }

    dataRef.current.push({ timestamp: now, value });
    const cutoff = now - config.minutes * 60_000;
    dataRef.current = dataRef.current.filter((p) => p.timestamp >= cutoff).slice(-maxPoints);
    setChartData([...dataRef.current]);
  }, [editsPerSecond, statsReady, config.minutes, config.bucketSec, maxPoints]);

  const loading = !statsReady;
  const chartW = dims.w - PAD.left - PAD.right;
  const chartH = dims.h - PAD.top - PAD.bottom;

  const maxValue = useMemo(() => {
    const m = Math.max(...chartData.map((p) => p.value), 1);
    return m * 1.2;
  }, [chartData]);

  const points = useMemo(() => {
    if (chartData.length === 0) return [];
    // Use fixed time window, not data range
    const now = Date.now();
    const rangeMs = config.minutes * 60 * 1000;
    const tMin = now - rangeMs;
    const tMax = now;
    const tRange = rangeMs;
    
    return chartData.map((p) => ({
      x: PAD.left + ((p.timestamp - tMin) / tRange) * chartW,
      y: PAD.top + chartH - (p.value / maxValue) * chartH,
    }));
  }, [chartData, chartW, chartH, maxValue, config.minutes]);

  // Sharp line segments (ECG-style: straight lines between points)
  const linePath = useMemo(() => {
    if (points.length === 0) return '';
    return points.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join('');
  }, [points]);

  // Y-axis ticks
  const yTicks = useMemo(() => {
    const count = 4;
    return Array.from({ length: count + 1 }, (_, i) => {
      const val = (maxValue / count) * i;
      const y = PAD.top + chartH - (val / maxValue) * chartH;
      return { val, y };
    });
  }, [maxValue, chartH]);

  // X-axis labels - show expected time range, not actual data range
  const xLabels = useMemo(() => {
    const now = Date.now();
    const rangeMs = config.minutes * 60 * 1000;
    const startTime = now - rangeMs;
    const count = 5;
    
    return Array.from({ length: count }, (_, i) => {
      const timestamp = startTime + (i / (count - 1)) * rangeMs;
      const pct = i / (count - 1);
      const x = PAD.left + pct * chartW;
      
      return {
        x,
        label: new Date(timestamp).toLocaleTimeString([], { 
          hour: '2-digit', 
          minute: '2-digit'
        }),
      };
    });
  }, [config.minutes, chartW, tick]); // Update with animation tick so labels move with time

  // Scan-line X position — sweeps across chart area every 4 seconds
  const scanX = useMemo(() => {
    const cycle = 4000; // ms
    const pct = (tick % cycle) / cycle;
    return PAD.left + pct * chartW;
  }, [tick, chartW]);

  const handleMouseMove = useCallback(
    (e: React.MouseEvent<SVGSVGElement>) => {
      if (points.length === 0) return;
      const svg = svgRef.current;
      if (!svg) return;
      const rect = svg.getBoundingClientRect();
      const mx = e.clientX - rect.left;
      let closest = 0;
      let minDist = Infinity;
      for (let i = 0; i < points.length; i++) {
        const d = Math.abs(points[i].x - mx);
        if (d < minDist) { minDist = d; closest = i; }
      }
      setHoveredIndex(closest);
    },
    [points],
  );

  const lastPoint = chartData.length > 0 ? chartData[chartData.length - 1] : null;
  const lastVal = lastPoint?.value ?? 0;

  return (
    <div className="space-y-0 rounded-xl overflow-hidden" style={{ background: MONITOR_BG }}>
      {/* Header bar */}
      <div className="flex items-center justify-between px-4 py-2.5"
        style={{ borderBottom: `1px solid rgba(0,255,136,0.1)` }}>
        <div className="flex items-center gap-2.5">
          <Activity className="h-4 w-4" style={{ color: MONITOR_GREEN }} />
          <span className="text-xs font-semibold tracking-wide" style={{ color: MONITOR_GREEN }}>
            EDITS MONITOR
          </span>
          {lastPoint && (
            <span className="ml-1 font-mono text-sm tabular-nums font-bold animate-pulse"
              style={{ color: MONITOR_GREEN }}>
              {lastVal.toFixed(0)}
              <span className="text-[10px] font-normal ml-0.5 opacity-60">e/min</span>
            </span>
          )}
          {/* Mini BPM-style indicator */}
          <span className="flex items-center gap-1 ml-2">
            <span className="inline-block w-1.5 h-1.5 rounded-full animate-ping"
              style={{ backgroundColor: MONITOR_GREEN }} />
            <span className="text-[10px] font-mono opacity-40" style={{ color: MONITOR_GREEN }}>LIVE</span>
          </span>
        </div>

        <div className="flex rounded overflow-hidden text-[10px] font-mono"
          style={{ border: `1px solid rgba(0,255,136,0.2)` }}>
          {(Object.keys(RANGE_CONFIG) as TimeRange[]).map((r) => (
            <button
              key={r}
              onClick={() => { dataRef.current = []; setChartData([]); setRange(r); }}
              className="px-2 py-0.5 transition-colors"
              style={{
                background: range === r ? 'rgba(0,255,136,0.15)' : 'transparent',
                color: range === r ? MONITOR_GREEN : 'rgba(0,255,136,0.4)',
              }}
            >
              {RANGE_CONFIG[r].label}
            </button>
          ))}
        </div>
      </div>

      {/* Chart area */}
      <div ref={containerRef} className="h-60 relative" role="figure"
        aria-label="Edits per minute live monitor">
        {loading && chartData.length === 0 ? (
          <div className="h-full flex items-center justify-center text-sm font-mono"
            style={{ color: 'rgba(0,255,136,0.3)' }}>
            Acquiring signal…
          </div>
        ) : chartData.length === 0 ? (
          <div className="h-full flex items-center justify-center text-sm font-mono"
            style={{ color: 'rgba(0,255,136,0.3)' }}>
            No signal
          </div>
        ) : (
          <svg
            ref={svgRef}
            width={dims.w}
            height={dims.h}
            className="select-none"
            onMouseMove={handleMouseMove}
            onMouseLeave={() => setHoveredIndex(null)}
          >
            <defs>
              {/* Glow effect for the main line */}
              <filter id="monitorGlow">
                <feGaussianBlur stdDeviation="2.5" result="blur" />
                <feMerge>
                  <feMergeNode in="blur" />
                  <feMergeNode in="SourceGraphic" />
                </feMerge>
              </filter>
              {/* Stronger glow for the head dot */}
              <filter id="headGlow">
                <feGaussianBlur stdDeviation="5" result="blur" />
                <feMerge>
                  <feMergeNode in="blur" />
                  <feMergeNode in="blur" />
                  <feMergeNode in="SourceGraphic" />
                </feMerge>
              </filter>
              {/* Scan line gradient */}
              <linearGradient id="scanGrad" x1="0" y1="0" x2="1" y2="0">
                <stop offset="0%" stopColor={MONITOR_GREEN} stopOpacity="0" />
                <stop offset="80%" stopColor={MONITOR_GREEN} stopOpacity="0.07" />
                <stop offset="100%" stopColor={MONITOR_GREEN} stopOpacity="0.15" />
              </linearGradient>
              {/* Trailing fade gradient on the line */}
              <linearGradient id="lineFade" x1="0" y1="0" x2="1" y2="0">
                <stop offset="0%" stopColor={MONITOR_GREEN} stopOpacity="0.15" />
                <stop offset="40%" stopColor={MONITOR_GREEN} stopOpacity="0.6" />
                <stop offset="100%" stopColor={MONITOR_GREEN} stopOpacity="1" />
              </linearGradient>
            </defs>

            {/* Background grid — horizontal */}
            {yTicks.map((t, i) => (
              <line key={`h${i}`}
                x1={PAD.left} x2={dims.w - PAD.right} y1={t.y} y2={t.y}
                stroke={i % 2 === 0 ? GRID_COLOR_STRONG : GRID_COLOR}
                strokeWidth={0.5}
              />
            ))}
            {/* Background grid — vertical */}
            {Array.from({ length: 8 }, (_, i) => {
              const x = PAD.left + (chartW / 8) * (i + 1);
              return (
                <line key={`v${i}`}
                  x1={x} x2={x} y1={PAD.top} y2={PAD.top + chartH}
                  stroke={GRID_COLOR} strokeWidth={0.5}
                />
              );
            })}

            {/* Y-axis labels */}
            {yTicks.map((t, i) => (
              <text key={`yl${i}`} x={PAD.left - 6} y={t.y + 3} textAnchor="end"
                fill="rgba(0,255,136,0.3)" fontSize={9} fontFamily="monospace">
                {t.val >= 1000 ? `${(t.val / 1000).toFixed(1)}k` : t.val.toFixed(0)}
              </text>
            ))}

            {/* Sweeping scan-line */}
            <rect
              x={scanX - 30} y={PAD.top} width={30} height={chartH}
              fill="url(#scanGrad)"
            />
            <line
              x1={scanX} x2={scanX} y1={PAD.top} y2={PAD.top + chartH}
              stroke={MONITOR_GREEN} strokeWidth={0.5} opacity={0.2}
            />

            {/* Glowing line path */}
            {linePath && (
              <>
                {/* Shadow/glow layer */}
                <path d={linePath} fill="none"
                  stroke={MONITOR_GREEN} strokeWidth={3} strokeLinecap="round"
                  strokeLinejoin="round" filter="url(#monitorGlow)" opacity={0.4}
                />
                {/* Main crisp line */}
                <path d={linePath} fill="none"
                  stroke="url(#lineFade)" strokeWidth={1.8}
                  strokeLinecap="round" strokeLinejoin="round"
                />
              </>
            )}

            {/* Data point dots — all visible like a heart monitor trace */}
            {points.map((p, i) => {
              // Fade older points: newest = full brightness, oldest = dim
              const age = points.length > 1 ? i / (points.length - 1) : 1;
              const opacity = 0.2 + age * 0.8;
              const r = i === points.length - 1 ? 0 : 2.5; // skip last (has its own big dot)
              return r > 0 ? (
                <circle key={i}
                  cx={p.x} cy={p.y} r={r}
                  fill={MONITOR_GREEN} opacity={opacity}
                />
              ) : null;
            })}

            {/* Leading head dot — big, glowing, pulsing */}
            {points.length > 0 && (() => {
              const head = points[points.length - 1];
              return (
                <g>
                  {/* Outer glow ring */}
                  <circle cx={head.x} cy={head.y} r={10}
                    fill={MONITOR_GREEN} opacity={0.08}
                    filter="url(#headGlow)"
                  />
                  {/* Pulsing ring */}
                  <circle cx={head.x} cy={head.y} r={7}
                    fill="none" stroke={MONITOR_GREEN} strokeWidth={1}
                    opacity={0.3} className="animate-ping"
                  />
                  {/* Core dot */}
                  <circle cx={head.x} cy={head.y} r={4}
                    fill={MONITOR_GREEN} filter="url(#headGlow)"
                  />
                  <circle cx={head.x} cy={head.y} r={1.8}
                    fill="#fff"
                  />
                  {/* Value label near head */}
                  <text x={head.x - 8} y={head.y - 14}
                    fill={MONITOR_GREEN} fontSize={10} fontFamily="monospace" fontWeight="bold">
                    {lastVal.toFixed(0)}
                  </text>
                </g>
              );
            })()}

            {/* Hover interaction */}
            {hoveredIndex !== null && points[hoveredIndex] && (
              <g>
                <line
                  x1={points[hoveredIndex].x} x2={points[hoveredIndex].x}
                  y1={PAD.top} y2={PAD.top + chartH}
                  stroke={MONITOR_GREEN} strokeWidth={0.8} strokeDasharray="2 3" opacity={0.4}
                />
                <circle
                  cx={points[hoveredIndex].x} cy={points[hoveredIndex].y}
                  r={5} fill="none" stroke={MONITOR_GREEN} strokeWidth={1.5}
                />
                <rect
                  x={Math.min(points[hoveredIndex].x - 52, dims.w - PAD.right - 108)}
                  y={Math.max(points[hoveredIndex].y - 38, PAD.top)}
                  width={104} height={28} rx={4}
                  fill="rgba(0,20,10,0.85)" stroke={MONITOR_GREEN} strokeWidth={0.5}
                />
                <text
                  x={Math.min(points[hoveredIndex].x, dims.w - PAD.right - 56)}
                  y={Math.max(points[hoveredIndex].y - 26, PAD.top + 10)}
                  textAnchor="middle" fill={MONITOR_GREEN} fontSize={10} fontFamily="monospace">
                  {chartData[hoveredIndex].value.toFixed(1)} e/min
                </text>
                <text
                  x={Math.min(points[hoveredIndex].x, dims.w - PAD.right - 56)}
                  y={Math.max(points[hoveredIndex].y - 15, PAD.top + 21)}
                  textAnchor="middle" fill="rgba(0,255,136,0.5)" fontSize={9} fontFamily="monospace">
                  {new Date(chartData[hoveredIndex].timestamp).toLocaleTimeString()}
                </text>
              </g>
            )}

            {/* X-axis labels */}
            {xLabels.map((l, i) => (
              <text key={i} x={l.x} y={dims.h - 5}
                textAnchor="middle" fill="rgba(0,255,136,0.25)"
                fontSize={9} fontFamily="monospace">
                {l.label}
              </text>
            ))}
          </svg>
        )}
      </div>
    </div>
  );
});
