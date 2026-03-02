import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SeverityBadge } from '../components/Alerts/SeverityBadge';

describe('SeverityBadge', () => {
  it('renders the severity text in uppercase', () => {
    render(<SeverityBadge severity="critical" />);
    expect(screen.getByText('critical')).toBeInTheDocument();
    // The component uses CSS uppercase rather than JS toUpperCase
    const el = screen.getByText('critical');
    expect(el.className).toContain('uppercase');
  });

  it('applies red classes for critical', () => {
    const { container } = render(<SeverityBadge severity="critical" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('bg-red-900/30');
    expect(badge.className).toContain('text-red-400');
  });

  it('applies amber classes for high', () => {
    const { container } = render(<SeverityBadge severity="high" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('bg-amber-900/30');
    expect(badge.className).toContain('text-amber-400');
  });

  it('applies emerald classes for medium', () => {
    const { container } = render(<SeverityBadge severity="medium" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('bg-emerald-900/30');
    expect(badge.className).toContain('text-emerald-400');
  });

  it('applies cyan classes for low', () => {
    const { container } = render(<SeverityBadge severity="low" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('bg-cyan-900/30');
    expect(badge.className).toContain('text-cyan-400');
  });

  it('falls back to low style for unknown severity', () => {
    const { container } = render(<SeverityBadge severity="unknown" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('bg-cyan-900/30');
  });

  it('handles case-insensitive severity', () => {
    const { container } = render(<SeverityBadge severity="CRITICAL" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('bg-red-900/30');
  });

  it('renders as a small pill badge', () => {
    const { container } = render(<SeverityBadge severity="high" />);
    const badge = container.firstChild as HTMLElement;
    expect(badge.className).toContain('rounded-full');
    expect(badge.className).toContain('inline-flex');
  });
});
