import { useCallback, memo } from 'react';
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Cell,
  PieChart,
  Pie,
  Legend,
} from 'recharts';
import { Globe } from 'lucide-react';
import type { Stats, LanguageStat } from '../../types';
import { usePollingData } from '../../hooks/usePolling';
import { getStats } from '../../utils/api';

// ── Colors ────────────────────────────────────────────────────────
const LANG_COLORS = [
  '#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6',
  '#ec4899', '#06b6d4', '#f97316', '#14b8a6', '#6366f1',
];

// ── Fallback: derive language stats from stats.top_languages or mock
function deriveLanguages(stats: Stats): LanguageStat[] {
  if (stats.top_languages && stats.top_languages.length > 0) {
    return stats.top_languages.slice(0, 10);
  }
  // If the API doesn't return top_languages, return a placeholder
  return [];
}

// ── Custom tooltip ────────────────────────────────────────────────
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function BarTooltip({ active, payload }: any) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload as LanguageStat;
  return (
    <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg px-3 py-2 text-sm">
      <p className="font-semibold text-gray-900 dark:text-white">{d.language}</p>
      <p className="text-gray-500 dark:text-gray-400">
        {d.count.toLocaleString()} edits ({d.percentage.toFixed(1)}%)
      </p>
    </div>
  );
}

// ── Custom pie label ──────────────────────────────────────────────
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function renderPieLabel({ cx, cy, midAngle, innerRadius, outerRadius, percent, name }: any) {
  if (percent < 0.05) return null; // skip tiny slices
  const RADIAN = Math.PI / 180;
  const radius = innerRadius + (outerRadius - innerRadius) * 1.3;
  const x = cx + radius * Math.cos(-midAngle * RADIAN);
  const y = cy + radius * Math.sin(-midAngle * RADIAN);

  return (
    <text
      x={x}
      y={y}
      fill="#374151"
      textAnchor={x > cx ? 'start' : 'end'}
      dominantBaseline="central"
      fontSize={11}
    >
      {name} ({(percent * 100).toFixed(0)}%)
    </text>
  );
}

// ── Edit distribution (human vs bot) pie chart ────────────────────
const EditDistributionPie = memo(function EditDistributionPie({
  human,
  bot,
}: {
  human: number;
  bot: number;
}) {
  const total = human + bot;
  if (total === 0) return null;

  const data = [
    { name: 'Human', value: human },
    { name: 'Bot', value: bot },
  ];
  const colors = ['#3b82f6', '#9ca3af'];

  return (
    <div className="h-48">
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie
            data={data}
            cx="50%"
            cy="50%"
            outerRadius={65}
            dataKey="value"
            label={renderPieLabel}
            isAnimationActive={true}
            animationDuration={400}
          >
            {data.map((_, idx) => (
              <Cell key={idx} fill={colors[idx]} />
            ))}
          </Pie>
          <Legend
            verticalAlign="bottom"
            height={24}
            formatter={(value: string) => (
              <span className="text-xs text-gray-600 dark:text-gray-400">{value}</span>
            )}
          />
          <Tooltip
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            formatter={(value: any, name: any) => [
              `${Number(value).toLocaleString()} (${((Number(value) / total) * 100).toFixed(1)}%)`,
              name,
            ]}
          />
        </PieChart>
      </ResponsiveContainer>
    </div>
  );
});

// ── Main component ────────────────────────────────────────────────
export const LanguageDistributionChart = memo(function LanguageDistributionChart() {
  const fetchFn = useCallback(async () => {
    const stats = await getStats();
    return stats;
  }, []);

  const { data: stats, loading } = usePollingData<Stats>({
    fetchFunction: fetchFn,
    interval: 10_000,
  });

  const languages = stats ? deriveLanguages(stats) : [];
  const hasDistribution = stats?.edits_by_type && (stats.edits_by_type.human > 0 || stats.edits_by_type.bot > 0);

  return (
    <div className="card space-y-4">
      {/* Header */}
      <div className="flex items-center gap-2">
        <Globe className="h-4 w-4 text-gray-400" />
        <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Language Distribution</h3>
      </div>

      {loading && languages.length === 0 ? (
        <div className="h-64 flex items-center justify-center text-gray-400 text-sm">
          Loading language data…
        </div>
      ) : languages.length === 0 ? (
        <div className="h-64 flex items-center justify-center text-gray-400 text-sm">
          No language data available yet
        </div>
      ) : (
        <div className="h-64">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart
              data={languages}
              margin={{ top: 5, right: 10, left: 0, bottom: 5 }}
              layout="vertical"
            >
              <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" horizontal={false} />
              <XAxis
                type="number"
                tick={{ fontSize: 11, fill: '#9ca3af' }}
                stroke="#e5e7eb"
              />
              <YAxis
                dataKey="language"
                type="category"
                tick={{ fontSize: 11, fill: '#6b7280' }}
                stroke="#e5e7eb"
                width={40}
              />
              <Tooltip content={<BarTooltip />} />
              <Bar
                dataKey="count"
                radius={[0, 4, 4, 0]}
                isAnimationActive={true}
                animationDuration={400}
              >
                {languages.map((_, idx) => (
                  <Cell key={idx} fill={LANG_COLORS[idx % LANG_COLORS.length]} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}

      {/* Human vs Bot pie */}
      {hasDistribution && (
        <div className="border-t border-gray-100 dark:border-gray-700 pt-4">
          <h4 className="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wide mb-2">
            Edit Type Distribution
          </h4>
          <EditDistributionPie
            human={stats!.edits_by_type!.human}
            bot={stats!.edits_by_type!.bot}
          />
        </div>
      )}
    </div>
  );
});
