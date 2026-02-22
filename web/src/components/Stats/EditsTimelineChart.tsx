import { useState, useEffect, useRef, useMemo, memo } from 'react';
import { Activity } from 'lucide-react';
import type { TimeRange, TimeSeriesPoint } from '../../types';
import { useAppStore } from '../../store/appStore';
import { getTimeline } from '../../utils/api';

const RANGE_CONFIG: Record<TimeRange, { label: string; minutes: number; bucketSec: number }> = {
  '1h':  { label: '1H',  minutes: 60,   bucketSec: 30 },
  '6h':  { label: '6H',  minutes: 360,  bucketSec: 90 },
  '24h': { label: '24H', minutes: 1440, bucketSec: 300 },
};

const PAD = { top: 18, right: 20, bottom: 26, left: 44 };
const MONITOR_GREEN = '#00ff88';
const GRID_COLOR = 'rgba(0,255,136,0.06)';
const GRID_COLOR_STRONG = 'rgba(0,255,136,0.12)';

export const EditsTimelineChart = memo(function EditsTimelineChart() {
  const [range, setRange] = useState<TimeRange>('1h');
  const dataRef = useRef<TimeSeriesPoint[]>([]);
  const [chartData, setChartData] = useState<TimeSeriesPoint[]>([]);
  const svgRef = useRef<SVGSVGElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState({ w: 600, h: 240 });
  const [tick, setTick] = useState(0); // animation driver
  
  // Get stats from global store (shared with StatsOverview)
  const stats = useAppStore((s) => s.stats);
  const statsRef = useRef(stats);
  statsRef.current = stats;

  // Animation tick for scan-line sweep - 24 FPS for smooth animation
  useEffect(() => {
    let interval: ReturnType<typeof setInterval>;
    const loop = () => {
      setTick(Date.now());
    };
    interval = setInterval(loop, 42); // ~24 FPS (1000ms / 24 ≈ 42ms)
    return () => clearInterval(interval);
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

  const statsReady = stats !== null;
  const config = RANGE_CONFIG[range];
  const maxPoints = Math.floor(config.minutes * 60 / config.bucketSec);

  // Load historical timeline data then start live updates
  // Combined into one effect to avoid race conditions where live-point
  // effect would clear historical data before it finished loading.
  useEffect(() => {
    if (!statsReady) return;

    let cancelled = false;
    let interval: ReturnType<typeof setInterval> | undefined;

    // Clear old data when range/config changes
    dataRef.current = [];
    setChartData([]);

    const addPoint = () => {
      const now = Date.now();
      const currentStats = statsRef.current;
      const value = currentStats?.edits_per_second ? currentStats.edits_per_second * 60 : 0;

      dataRef.current.push({ timestamp: now, value });
      const cutoff = now - config.minutes * 60_000;
      dataRef.current = dataRef.current.filter((p) => p.timestamp >= cutoff).slice(-maxPoints);
      setChartData([...dataRef.current]);
    };

    const loadAndStart = async () => {
      try {
        const duration = `${config.minutes * 60}s`; // Convert to seconds
        const historicalPoints = await getTimeline(duration);
        if (cancelled) return;

        // Convert API response to TimeSeriesPoint format
        const points: TimeSeriesPoint[] = historicalPoints.map(p => ({
          timestamp: p.timestamp,
          value: p.value,
        }));

        // Filter to current range and aggregate to correct bucket size
        const now = Date.now();
        const cutoff = now - config.minutes * 60_000;
        const filtered = points.filter(p => p.timestamp >= cutoff);

        // Aggregate minute-level data into buckets
        const bucketMap = new Map<number, number>();
        for (const point of filtered) {
          const bucketTime = Math.floor(point.timestamp / (config.bucketSec * 1000)) * config.bucketSec * 1000;
          const current = bucketMap.get(bucketTime) || 0;
          bucketMap.set(bucketTime, current + point.value);
        }

        // Convert back to array and sort
        const aggregated = Array.from(bucketMap.entries())
          .map(([timestamp, value]) => ({ timestamp, value }))
          .sort((a, b) => a.timestamp - b.timestamp);

        if (cancelled) return;
        dataRef.current = aggregated;
        setChartData([...dataRef.current]);
      } catch (error) {
        console.error('Failed to load timeline data:', error);
        // Continue with empty data - will populate with new points
      }

      if (cancelled) return;

      // Add one live point immediately, then at regular intervals
      addPoint();
      interval = setInterval(addPoint, config.bucketSec * 1000);
    };

    loadAndStart();

    return () => {
      cancelled = true;
      if (interval) clearInterval(interval);
    };
  }, [statsReady, config.minutes, config.bucketSec, maxPoints]);

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
    const tRange = rangeMs;
    
    return chartData.map((p) => ({
      x: PAD.left + ((p.timestamp - tMin) / tRange) * chartW,
      y: PAD.top + chartH - (p.value / maxValue) * chartH,
    }));
  }, [chartData, chartW, chartH, maxValue, config.minutes, Math.floor(tick / 1000)]);

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

  // X-axis labels - show expected time range, update every second
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
  }, [config.minutes, chartW, Math.floor(tick / 1000)]); // Update every second

  // Scan-line X position — sweeps across chart area every 4 seconds
  const scanX = useMemo(() => {
    const cycle = 4000; // ms
    const pct = (tick % cycle) / cycle;
    return PAD.left + pct * chartW;
  }, [tick, chartW]);

  const lastPoint = chartData.length > 0 ? chartData[chartData.length - 1] : null;
  const lastVal = lastPoint?.value ?? 0;

  return (
    <div className="card flex flex-col space-y-0 rounded-xl overflow-hidden !p-0">
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
      <div ref={containerRef} className="flex-1 min-h-[11rem] relative px-3 pb-3 pt-1" role="figure"
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
            className="select-none block"
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
                </g>
              );
            })()}

            {/* X-axis labels */}
            {xLabels.map((l, i) => (
              <text key={i} x={l.x} y={dims.h - 6}
                textAnchor={i === 0 ? 'start' : i === xLabels.length - 1 ? 'end' : 'middle'}
                fill="rgba(0,255,136,0.25)"
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
