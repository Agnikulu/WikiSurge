import { useCallback, useState } from 'react';
import type { EditWar, EditWarAnalysis } from '../../types';
import { getEditWarAnalysis } from '../../utils/api';
import {
  Brain,
  AlertTriangle,
  Lightbulb,
  RefreshCw,
  Loader2,
  Zap,
  ChevronRight,
  Shield,
  Users,
  MessageSquare,
} from 'lucide-react';

interface EditWarAnalysisCardProps {
  war: EditWar;
}

const severityConfig: Record<string, { color: string; bg: string; border: string; label: string }> = {
  critical: { color: '#f87171', bg: 'rgba(239, 68, 68, 0.1)', border: 'rgba(239, 68, 68, 0.3)', label: 'CRITICAL' },
  high:     { color: '#fb923c', bg: 'rgba(251, 146, 60, 0.1)', border: 'rgba(251, 146, 60, 0.3)', label: 'HIGH' },
  moderate: { color: '#facc15', bg: 'rgba(250, 204, 21, 0.1)', border: 'rgba(250, 204, 21, 0.3)', label: 'MODERATE' },
  low:      { color: '#4ade80', bg: 'rgba(74, 222, 128, 0.1)', border: 'rgba(74, 222, 128, 0.3)', label: 'LOW' },
  unknown:  { color: '#94a3b8', bg: 'rgba(148, 163, 184, 0.1)', border: 'rgba(148, 163, 184, 0.3)', label: 'UNKNOWN' },
};

export function EditWarAnalysisCard({ war }: EditWarAnalysisCardProps) {
  const [analysis, setAnalysis] = useState<EditWarAnalysis | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);

  const fetchAnalysis = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await getEditWarAnalysis(war.page_title);
      setAnalysis(result);
      setExpanded(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch analysis');
    } finally {
      setLoading(false);
    }
  }, [war.page_title]);

  // Not yet loaded — show CTA button
  if (!analysis && !loading && !error) {
    return (
      <button
        onClick={fetchAnalysis}
        className="w-full flex items-center gap-2 px-3 py-2 text-xs font-medium rounded border transition-all hover:shadow-sm"
        style={{
          background: 'rgba(139, 92, 246, 0.08)',
          borderColor: 'rgba(139, 92, 246, 0.2)',
          color: '#a78bfa',
          fontFamily: 'monospace',
        }}
      >
        <Brain className="h-3.5 w-3.5" />
        <span>ANALYZE CONFLICT</span>
        <ChevronRight className="h-3 w-3 ml-auto" />
      </button>
    );
  }

  // Loading state
  if (loading) {
    return (
      <div
        className="w-full flex items-center gap-2 px-3 py-3 text-xs rounded border animate-pulse"
        style={{
          background: 'rgba(139, 92, 246, 0.05)',
          borderColor: 'rgba(139, 92, 246, 0.15)',
          color: '#a78bfa',
          fontFamily: 'monospace',
        }}
      >
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        <span>Analyzing edit war conflict...</span>
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="w-full space-y-1">
        <div
          className="flex items-center gap-2 px-3 py-2 text-xs rounded border"
          style={{
            background: 'rgba(239, 68, 68, 0.05)',
            borderColor: 'rgba(239, 68, 68, 0.2)',
            color: '#f87171',
            fontFamily: 'monospace',
          }}
        >
          <AlertTriangle className="h-3.5 w-3.5" />
          <span>{error}</span>
          <button
            onClick={fetchAnalysis}
            className="ml-auto p-0.5 rounded hover:bg-red-900/20 transition-colors"
          >
            <RefreshCw className="h-3 w-3" />
          </button>
        </div>
      </div>
    );
  }

  // Analysis loaded
  if (!analysis) return null;

  const sev = severityConfig[analysis.severity] || severityConfig.unknown;

  return (
    <div className="w-full space-y-2">
      {/* Header */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-2 text-xs font-semibold uppercase tracking-wide rounded transition-colors"
        style={{
          background: 'rgba(139, 92, 246, 0.06)',
          color: '#a78bfa',
          fontFamily: 'monospace',
        }}
      >
        <Brain className="h-3.5 w-3.5" />
        <span>Conflict Analysis</span>
        {/* Severity badge */}
        <span
          className="text-[9px] font-bold px-1.5 py-0.5 rounded"
          style={{ background: sev.bg, color: sev.color, border: `1px solid ${sev.border}` }}
        >
          {sev.label}
        </span>
        {analysis.cache_hit && (
          <Zap className="h-3 w-3 text-yellow-500" title="Cached result" />
        )}
        <button
          onClick={(e) => {
            e.stopPropagation();
            fetchAnalysis();
          }}
          className="ml-auto p-0.5 rounded hover:bg-purple-900/20 transition-colors"
          title="Refresh analysis"
        >
          <RefreshCw className="h-3 w-3" />
        </button>
      </button>

      {/* Body */}
      {expanded && (
        <div className="space-y-3 px-3 pb-2 animate-slide-down">
          {/* Summary */}
          <p
            className="text-xs leading-relaxed"
            style={{ color: 'rgba(0, 255, 136, 0.7)', fontFamily: 'monospace' }}
          >
            {analysis.summary}
          </p>

          {/* Content area + edit count badges */}
          <div className="flex items-center gap-1.5 flex-wrap">
            {analysis.content_area && analysis.content_area !== 'unknown' && (
              <span
                className="text-[10px] font-medium px-2 py-0.5 rounded-full"
                style={{
                  background: 'rgba(139, 92, 246, 0.1)',
                  color: '#c4b5fd',
                  border: '1px solid rgba(139, 92, 246, 0.2)',
                }}
              >
                {analysis.content_area}
              </span>
            )}
            <span
              className="text-[10px] px-2 py-0.5 rounded-full"
              style={{
                background: 'rgba(0, 255, 136, 0.05)',
                color: 'rgba(0, 255, 136, 0.4)',
                border: '1px solid rgba(0, 255, 136, 0.1)',
              }}
            >
              {analysis.edit_count} edits analyzed
            </span>
          </div>

          {/* Recommendation */}
          {analysis.recommendation && (
            <div
              className="flex items-start gap-2 px-2.5 py-2 rounded text-[11px] leading-snug"
              style={{
                background: 'rgba(59, 130, 246, 0.06)',
                border: '1px solid rgba(59, 130, 246, 0.15)',
                color: 'rgba(147, 197, 253, 0.8)',
                fontFamily: 'monospace',
              }}
            >
              <Shield className="h-3.5 w-3.5 mt-0.5 shrink-0" style={{ color: 'rgba(59, 130, 246, 0.6)' }} />
              <span>{analysis.recommendation}</span>
            </div>
          )}

          {/* Opposing positions */}
          {analysis.positions.length > 0 && (
            <div className="space-y-1.5">
              <h5
                className="text-[10px] font-semibold uppercase tracking-wide flex items-center gap-1"
                style={{ color: 'rgba(139, 92, 246, 0.6)', fontFamily: 'monospace' }}
              >
                <Lightbulb className="h-3 w-3" />
                Opposing Positions
              </h5>
              <div className="space-y-1">
                {analysis.positions.map((position, idx) => (
                  <div
                    key={idx}
                    className="flex items-start gap-2 text-[11px] leading-snug pl-2"
                    style={{
                      color: 'rgba(0, 255, 136, 0.55)',
                      fontFamily: 'monospace',
                      borderLeft: `2px solid ${idx === 0 ? 'rgba(59, 130, 246, 0.4)' : idx === 1 ? 'rgba(239, 68, 68, 0.4)' : 'rgba(234, 179, 8, 0.4)'}`,
                    }}
                  >
                    <span className="pt-0.5">{position}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Key editors */}
          {analysis.key_editors && analysis.key_editors.length > 0 && (
            <div className="space-y-1.5">
              <h5
                className="text-[10px] font-semibold uppercase tracking-wide flex items-center gap-1"
                style={{ color: 'rgba(139, 92, 246, 0.6)', fontFamily: 'monospace' }}
              >
                <Users className="h-3 w-3" />
                Key Editors
              </h5>
              <div className="flex flex-wrap gap-1.5">
                {analysis.key_editors.map((editor, idx) => (
                  <div
                    key={idx}
                    className="flex items-center gap-1.5 text-[10px] px-2 py-1 rounded"
                    style={{
                      background: 'rgba(0, 255, 136, 0.04)',
                      border: '1px solid rgba(0, 255, 136, 0.1)',
                      color: 'rgba(0, 255, 136, 0.5)',
                      fontFamily: 'monospace',
                    }}
                  >
                    <MessageSquare className="h-2.5 w-2.5" />
                    <span style={{ color: 'rgba(0, 255, 136, 0.7)' }}>{editor.user}</span>
                    <span className="opacity-50">·</span>
                    <span>{editor.edit_count} edits</span>
                    <span className="opacity-50">·</span>
                    <span
                      className="italic"
                      style={{ color: 'rgba(139, 92, 246, 0.5)' }}
                    >
                      {editor.role}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Generated time */}
          <div
            className="text-[9px] text-right"
            style={{ color: 'rgba(0, 255, 136, 0.2)', fontFamily: 'monospace' }}
          >
            {analysis.cache_hit ? 'cached · ' : ''}
            generated {new Date(analysis.generated_at).toLocaleTimeString()}
          </div>
        </div>
      )}
    </div>
  );
}
