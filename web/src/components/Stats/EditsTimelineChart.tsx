import { useState, useCallback, useRef, memo } from 'react';
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  type TooltipProps,
} from 'recharts';
import { Clock } from 'lucide-react';
import type { TimeRange, TimeSeriesPoint } from '../../types';
import { usePollingData } from '../../hooks/usePolling';
import { getStats } from '../../utils/api';

// ── helpers ───────────────────────────────────────────────────────
const RANGE_CONFIG: Record<TimeRange, { label: string; minutes: number; bucketSec: number }> = {
  '1h':  { label: '1 Hour',   minutes: 60,   bucketSec: 60 },
  '6h':  { label: '6 Hours',  minutes: 360,  bucketSec: 360 },
  '24h': { label: '24 Hours', minutes: 1440, bucketSec: 1440 },
};

function formatTimeLabel(ts: number, range: TimeRange): string {
  const d = new Date(ts);
  if (range === '24h') {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

// ── Custom tooltip ────────────────────────────────────────────────
function ChartTooltip({ active, payload, label }: TooltipProps<number, string>) {
  if (!active || !payload?.length) return null;
  return (
    <div className="bg-white border border-gray-200 rounded-lg shadow-lg px-3 py-2 text-sm">
      <p className="text-gray-500 text-xs mb-1">
        {new Date(label as number).toLocaleTimeString()}
      </p>
      <p className="font-semibold text-gray-900">
        {payload[0].value?.toFixed(1)} edits/min
      </p>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────
export const EditsTimelineChart = memo(function EditsTimelineChart() {
  const [range, setRange] = useState<TimeRange>('1h');
  const dataRef = useRef<TimeSeriesPoint[]>([]);

  const config = RANGE_CONFIG[range];
  const maxPoints = Math.floor(config.minutes * 60 / config.bucketSec);

  // Build a synthetic timeline from polling the stats endpoint
  const fetchFn = useCallback(async () => {
    const stats = await getStats();
    const now = Date.now();
    const point: TimeSeriesPoint = { timestamp: now, value: stats.edits_per_second * 60 };

    // Append & trim
    const cutoff = now - config.minutes * 60_000;
    dataRef.current = [...dataRef.current.filter((p) => p.timestamp >= cutoff), point].slice(
      -maxPoints,
    );

    return [...dataRef.current];
  }, [config.minutes, maxPoints]);

  const { data: points, loading } = usePollingData<TimeSeriesPoint[]>({
    fetchFunction: fetchFn,
    interval: config.bucketSec * 1000,
  });

  const chartData = points ?? [];

  return (
    <div className="card space-y-3">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Clock className="h-4 w-4 text-gray-400" />
          <h3 className="text-sm font-semibold text-gray-700">Edits Timeline</h3>
        </div>

        <div className="flex rounded-md overflow-hidden border border-gray-200 text-xs">
          {(Object.keys(RANGE_CONFIG) as TimeRange[]).map((r) => (
            <button
              key={r}
              onClick={() => {
                dataRef.current = [];
                setRange(r);
              }}
              className={`px-2.5 py-1 transition-colors ${
                range === r
                  ? 'bg-blue-600 text-white'
                  : 'bg-white text-gray-600 hover:bg-gray-50'
              }`}
            >
              {RANGE_CONFIG[r].label}
            </button>
          ))}
        </div>
      </div>

      {/* Chart */}
      <div className="h-64">
        {loading && chartData.length === 0 ? (
          <div className="h-full flex items-center justify-center text-gray-400 text-sm">
            Loading chart data…
          </div>
        ) : chartData.length < 2 ? (
          <div className="h-full flex items-center justify-center text-gray-400 text-sm">
            Collecting data points… ({chartData.length}/2 min)
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 5, right: 10, left: 0, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
              <XAxis
                dataKey="timestamp"
                type="number"
                domain={['dataMin', 'dataMax']}
                tickFormatter={(ts) => formatTimeLabel(ts, range)}
                tick={{ fontSize: 11, fill: '#9ca3af' }}
                stroke="#e5e7eb"
              />
              <YAxis
                tick={{ fontSize: 11, fill: '#9ca3af' }}
                stroke="#e5e7eb"
                width={40}
                label={{
                  value: 'edits/min',
                  angle: -90,
                  position: 'insideLeft',
                  style: { fontSize: 10, fill: '#9ca3af' },
                }}
              />
              <Tooltip content={<ChartTooltip />} />
              <Line
                type="monotone"
                dataKey="value"
                stroke="#3b82f6"
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4, fill: '#3b82f6' }}
                isAnimationActive={true}
                animationDuration={300}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
});
