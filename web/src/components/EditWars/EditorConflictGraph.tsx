import { useMemo } from 'react';
import type { EditWar } from '../../types';
import { RotateCcw, User } from 'lucide-react';

interface EditorConflictGraphProps {
  war: EditWar;
}

/** Deterministic color palette for editors */
const EDITOR_COLORS = [
  'from-blue-400 to-blue-600',
  'from-red-400 to-red-600',
  'from-green-400 to-green-600',
  'from-purple-400 to-purple-600',
  'from-orange-400 to-orange-600',
  'from-teal-400 to-teal-600',
  'from-pink-400 to-pink-600',
  'from-yellow-400 to-yellow-600',
];

const EDITOR_BG_COLORS = [
  'bg-blue-50 border-blue-200',
  'bg-red-50 border-red-200',
  'bg-green-50 border-green-200',
  'bg-purple-50 border-purple-200',
  'bg-orange-50 border-orange-200',
  'bg-teal-50 border-teal-200',
  'bg-pink-50 border-pink-200',
  'bg-yellow-50 border-yellow-200',
];

/**
 * Visual representation of editors in an edit war and their interactions.
 *
 * This is a "simple" implementation: list of editor nodes with visual
 * indicators for their participation level (based on available data).
 * If full revert-edge data were available, this could be upgraded to
 * a force-directed graph with D3 / vis.js.
 */
export function EditorConflictGraph({ war }: EditorConflictGraphProps) {
  const editors = war.editors;

  // Estimate per-editor share (we don't have per-editor stats, so distribute evenly)
  const avgEdits = war.edit_count > 0 ? Math.ceil(war.edit_count / editors.length) : 1;
  const avgReverts = war.revert_count > 0 ? Math.ceil(war.revert_count / editors.length) : 0;

  // Infer rough revert edges between editors (all-to-all for now)
  const edges = useMemo(() => {
    if (editors.length < 2) return [];
    const result: { from: string; to: string }[] = [];
    for (let i = 0; i < editors.length; i++) {
      for (let j = i + 1; j < editors.length; j++) {
        result.push({ from: editors[i], to: editors[j] });
      }
    }
    return result;
  }, [editors]);

  if (editors.length === 0) {
    return (
      <div className="text-sm text-center py-4" style={{ color: 'rgba(0,255,136,0.4)' }}>
        No editor data available
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Section title */}
      <h4 className="text-xs font-semibold uppercase tracking-wide" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}>
        Editor Conflict Map
      </h4>

      {/* Editor nodes */}
      <div className="flex flex-wrap gap-3 justify-center">
        {editors.map((editor, idx) => {
          const gradient = EDITOR_COLORS[idx % EDITOR_COLORS.length];
          const cardStyle = EDITOR_BG_COLORS[idx % EDITOR_BG_COLORS.length];
          const sizeClass =
            editors.length <= 3
              ? 'w-14 h-14 text-lg'
              : 'w-10 h-10 text-sm';

          return (
            <div
              key={editor}
              className={`flex flex-col items-center gap-1.5 p-3 rounded-lg border ${cardStyle} transition-transform hover:scale-105`}
            >
              {/* Avatar */}
              <div
                className={`${sizeClass} rounded-full bg-gradient-to-br ${gradient} flex items-center justify-center text-white font-bold shadow-sm`}
              >
                {editor.charAt(0).toUpperCase()}
              </div>

              {/* Name */}
              <a
                href={`https://en.wikipedia.org/wiki/User:${encodeURIComponent(editor.replace(/ /g, '_'))}`}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs font-medium hover:underline truncate max-w-[90px] text-center"
                style={{ color: 'rgba(0,255,136,0.7)' }}
                title={editor}
              >
                {editor}
              </a>

              {/* Stats */}
              <div className="flex items-center gap-2 text-[10px]" style={{ color: 'rgba(0,255,136,0.5)' }}>
                <span className="flex items-center gap-0.5">
                  <User className="h-2.5 w-2.5" />
                  ~{avgEdits} edits
                </span>
                {avgReverts > 0 && (
                  <span className="flex items-center gap-0.5">
                    <RotateCcw className="h-2.5 w-2.5" />
                    ~{avgReverts} rev
                  </span>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* Conflict edges */}
      {edges.length > 0 && (
        <div className="space-y-1">
          <h5 className="text-[10px] font-semibold uppercase" style={{ color: 'rgba(0,255,136,0.4)', fontFamily: 'monospace' }}>
            Revert Interactions
          </h5>
          <div className="flex flex-wrap gap-1.5">
            {edges.map(({ from, to }) => (
              <span
                key={`${from}-${to}`}
                className="inline-flex items-center gap-1 px-2 py-0.5 bg-orange-50 border border-orange-200 rounded-full text-[10px] text-orange-700"
              >
                <span className="font-medium truncate max-w-[60px]">{from}</span>
                <RotateCcw className="h-2.5 w-2.5" />
                <span className="font-medium truncate max-w-[60px]">{to}</span>
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
