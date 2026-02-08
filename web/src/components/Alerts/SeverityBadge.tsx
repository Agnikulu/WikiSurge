interface SeverityBadgeProps {
  severity: string;
}

const SEVERITY_STYLES: Record<string, string> = {
  critical: 'bg-red-100 text-red-800',
  high: 'bg-orange-100 text-orange-800',
  medium: 'bg-yellow-100 text-yellow-800',
  low: 'bg-blue-100 text-blue-800',
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
