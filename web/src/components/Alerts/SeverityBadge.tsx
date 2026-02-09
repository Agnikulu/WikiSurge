interface SeverityBadgeProps {
  severity: string;
}

const SEVERITY_STYLES: Record<string, string> = {
  critical: 'bg-red-900/30 text-red-400 border border-red-800/40',
  high: 'bg-amber-900/30 text-amber-400 border border-amber-800/40',
  medium: 'bg-emerald-900/30 text-emerald-400 border border-emerald-800/40',
  low: 'bg-cyan-900/30 text-cyan-400 border border-cyan-800/40',
};

/**
 * Reusable pill badge that displays a severity level with matching colors.
 */
export function SeverityBadge({ severity }: SeverityBadgeProps) {
  const key = severity.toLowerCase();
  const style = SEVERITY_STYLES[key] ?? SEVERITY_STYLES.low;

  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-semibold uppercase tracking-wide ${style}`}
    >
      {severity}
    </span>
  );
}
