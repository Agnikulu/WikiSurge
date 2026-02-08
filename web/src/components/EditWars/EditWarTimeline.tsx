import { useMemo, useState } from 'react';
import type { EditWar, EditWarTimelineEntry } from '../../types';
import { formatRelativeTime } from '../../utils/formatting';
import { Clock, Filter, RotateCcw, FileEdit, Download } from 'lucide-react';

interface EditWarTimelineProps {
  war: EditWar;
  /** Optional pre-loaded timeline entries. If not provided, a synthetic timeline is generated. */
  entries?: EditWarTimelineEntry[];
}

/** Deterministic color for an editor name */
const EDITOR_PALETTE = [
  { dot: 'bg-blue-500', text: 'text-blue-700', bg: 'bg-blue-50' },
  { dot: 'bg-red-500', text: 'text-red-700', bg: 'bg-red-50' },
  { dot: 'bg-green-500', text: 'text-green-700', bg: 'bg-green-50' },
  { dot: 'bg-purple-500', text: 'text-purple-700', bg: 'bg-purple-50' },
  { dot: 'bg-orange-500', text: 'text-orange-700', bg: 'bg-orange-50' },
  { dot: 'bg-teal-500', text: 'text-teal-700', bg: 'bg-teal-50' },
  { dot: 'bg-pink-500', text: 'text-pink-700', bg: 'bg-pink-50' },
  { dot: 'bg-yellow-500', text: 'text-yellow-700', bg: 'bg-yellow-50' },
];

function colorForEditor(editor: string, editorList: string[]) {
  const idx = editorList.indexOf(editor);
  return EDITOR_PALETTE[(idx >= 0 ? idx : 0) % EDITOR_PALETTE.length];
}

/**
 * Generate a synthetic timeline when real entries aren't available.
 * Spreads edits+reverts evenly across the war's duration.
 */
function generateSyntheticTimeline(war: EditWar): EditWarTimelineEntry[] {
  const startMs = new Date(war.start_time).getTime();
  const endMs = new Date(war.last_edit).getTime();
  const range = Math.max(endMs - startMs, 1);
  const total = war.edit_count;
  const entries: EditWarTimelineEntry[] = [];

  for (let i = 0; i < total; i++) {
    const t = startMs + (range / total) * i;
    const editor = war.editors[i % war.editors.length];
    const isRevert = i < war.revert_count;
    entries.push({
      timestamp: new Date(t).toISOString(),
      editor,
      action: isRevert ? 'revert' : 'edit',
      byte_change: isRevert ? -(Math.floor(Math.random() * 500) + 50) : Math.floor(Math.random() * 300) + 10,
    });
  }

  return entries;
}

export function EditWarTimeline({ war, entries }: EditWarTimelineProps) {
  const [editorFilter, setEditorFilter] = useState<string | null>(null);

  const timeline = useMemo(
    () => entries ?? generateSyntheticTimeline(war),
    [entries, war],
  );

  const filtered = useMemo(() => {
    if (!editorFilter) return timeline;
    return timeline.filter((e) => e.editor === editorFilter);
  }, [timeline, editorFilter]);

  // ── Export to CSV ─────────────────────────────────
  const handleExport = () => {
    const headers = 'Timestamp,Editor,Action,Byte Change,Comment\n';
    const rows = timeline
      .map(
        (e) =>
          `${e.timestamp},"${e.editor}",${e.action},${e.byte_change},"${e.comment ?? ''}"`,
      )
      .join('\n');
    const csv = headers + rows;
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `edit-war-timeline-${war.page_title.replace(/\s+/g, '_')}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  if (timeline.length === 0) {
    return (
      <div className="text-sm text-gray-400 text-center py-4">
        No timeline data available
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h4 className="text-xs font-semibold text-gray-500 uppercase tracking-wide flex items-center gap-1">
          <Clock className="h-3 w-3" />
          Edit Timeline
        </h4>

        <div className="flex items-center gap-2">
          {/* Editor filter */}
          <div className="relative">
            <select
              value={editorFilter ?? ''}
              onChange={(e) =>
                setEditorFilter(e.target.value || null)
              }
              className="text-[11px] border border-gray-200 rounded px-2 py-0.5 pr-5 bg-white text-gray-600 appearance-none cursor-pointer"
            >
              <option value="">All editors</option>
              {war.editors.map((ed) => (
                <option key={ed} value={ed}>
                  {ed}
                </option>
              ))}
            </select>
            <Filter className="absolute right-1.5 top-1/2 -translate-y-1/2 h-2.5 w-2.5 text-gray-400 pointer-events-none" />
          </div>

          {/* Export */}
          <button
            onClick={handleExport}
            className="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] text-gray-500 hover:text-gray-700 border border-gray-200 rounded hover:bg-gray-50 transition-colors"
            title="Export timeline as CSV"
          >
            <Download className="h-2.5 w-2.5" />
            CSV
          </button>
        </div>
      </div>

      {/* Visual timeline bar */}
      <TimelineStrip entries={filtered} editors={war.editors} />

      {/* Timeline list */}
      <div className="relative ml-4 border-l-2 border-gray-200 space-y-0 max-h-64 overflow-y-auto scrollbar-thin">
        {filtered.map((entry, idx) => {
          const color = colorForEditor(entry.editor, war.editors);
          const isRevert = entry.action === 'revert';

          return (
            <div
              key={idx}
              className="relative pl-5 py-1.5 group hover:bg-gray-50 transition-colors"
            >
              {/* Dot on timeline */}
              <span
                className={`absolute -left-[5px] top-3 w-2 h-2 rounded-full ${color.dot} ring-2 ring-white`}
              />

              <div className="flex items-center gap-2 text-xs">
                {/* Action icon */}
                {isRevert ? (
                  <RotateCcw className="h-3 w-3 text-red-500 flex-shrink-0" />
                ) : (
                  <FileEdit className="h-3 w-3 text-gray-400 flex-shrink-0" />
                )}

                {/* Editor */}
                <span className={`font-medium ${color.text}`}>
                  {entry.editor}
                </span>

                {/* Action label */}
                <span
                  className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                    isRevert
                      ? 'bg-red-50 text-red-600'
                      : 'bg-gray-100 text-gray-600'
                  }`}
                >
                  {isRevert ? 'Revert' : 'Edit'}
                </span>

                {/* Byte change */}
                <span
                  className={`text-[10px] ${
                    entry.byte_change >= 0 ? 'text-green-600' : 'text-red-600'
                  }`}
                >
                  {entry.byte_change >= 0 ? '+' : ''}
                  {entry.byte_change}
                </span>

                {/* Timestamp */}
                <span className="text-gray-400 ml-auto text-[10px]">
                  {formatRelativeTime(entry.timestamp)}
                </span>
              </div>

              {entry.comment && (
                <p className="text-[10px] text-gray-400 mt-0.5 truncate pl-5">
                  {entry.comment}
                </p>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/* ── Mini timeline strip ─────────────────────────────── */

function TimelineStrip({
  entries,
  editors,
}: {
  entries: EditWarTimelineEntry[];
  editors: string[];
}) {
  if (entries.length === 0) return null;

  const firstMs = new Date(entries[0].timestamp).getTime();
  const lastMs = new Date(entries[entries.length - 1].timestamp).getTime();
  const rangeMs = Math.max(lastMs - firstMs, 1);

  return (
    <div className="relative h-6 bg-gray-100 rounded-full overflow-hidden">
      {entries.map((entry, idx) => {
        const posMs = new Date(entry.timestamp).getTime() - firstMs;
        const leftPct = (posMs / rangeMs) * 100;
        const color = colorForEditor(entry.editor, editors);
        const isRevert = entry.action === 'revert';

        return (
          <span
            key={idx}
            className={`absolute top-1 ${
              isRevert ? 'w-2 h-4 rounded-sm' : 'w-1.5 h-3 rounded-full'
            } ${color.dot} opacity-80`}
            style={{ left: `${Math.min(leftPct, 98)}%` }}
            title={`${entry.editor}: ${entry.action} (${entry.byte_change > 0 ? '+' : ''}${entry.byte_change})`}
          />
        );
      })}
    </div>
  );
}
