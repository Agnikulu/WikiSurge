import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatCard } from '../components/Stats/StatCard';
import { Zap, Activity, TrendingUp, AlertTriangle, Globe, BarChart3 } from 'lucide-react';
import type { Trend } from '../components/Stats/StatCard';

describe('StatCard', () => {
  it('renders label and value', () => {
    render(<StatCard label="Edits/sec" value="7.3" icon={Zap} color="text-blue-500" />);
    expect(screen.getByText('Edits/sec')).toBeInTheDocument();
    expect(screen.getByText('7.3')).toBeInTheDocument();
  });

  it('renders the icon', () => {
    const { container } = render(
      <StatCard label="Edits Today" value="234,567" icon={Activity} color="text-green-500" />,
    );
    // lucide renders an <svg> element
    const svg = container.querySelector('svg');
    expect(svg).not.toBeNull();
  });

  it('applies accent color border class', () => {
    const { container } = render(
      <StatCard
        label="Hot Pages"
        value="145"
        icon={TrendingUp}
        color="text-orange-500"
        accentColor="border-orange-500"
      />,
    );
    const card = container.firstChild as HTMLElement;
    expect(card.className).toContain('border-orange-500');
  });

  it('falls back to gray border when no accentColor', () => {
    const { container } = render(
      <StatCard label="Test" value="0" icon={Zap} color="text-blue-500" />,
    );
    const card = container.firstChild as HTMLElement;
    expect(card.className).toContain('border-gray-200');
  });

  it('shows upward trend indicator in green', () => {
    const trend: Trend = { direction: 'up', value: 12.5 };
    render(
      <StatCard label="Edits/sec" value="7.3" icon={Zap} color="text-blue-500" trend={trend} />,
    );
    expect(screen.getByText('12.5%')).toBeInTheDocument();
    const trendEl = screen.getByText('12.5%').closest('span');
    expect(trendEl?.className).toContain('text-green-600');
  });

  it('shows downward trend indicator in red', () => {
    const trend: Trend = { direction: 'down', value: 3.2 };
    render(
      <StatCard label="Alerts" value="12" icon={AlertTriangle} color="text-red-500" trend={trend} />,
    );
    expect(screen.getByText('3.2%')).toBeInTheDocument();
    const trendEl = screen.getByText('3.2%').closest('span');
    expect(trendEl?.className).toContain('text-red-600');
  });

  it('does not render trend when direction is neutral', () => {
    const trend: Trend = { direction: 'neutral', value: 0 };
    render(
      <StatCard label="Trending" value="50" icon={BarChart3} color="text-purple-500" trend={trend} />,
    );
    expect(screen.queryByText('0.0%')).not.toBeInTheDocument();
  });

  it('does not render trend indicator when trend is undefined', () => {
    render(
      <StatCard label="Top Language" value="en" icon={Globe} color="text-indigo-500" />,
    );
    // No percentage text should appear
    const percentElements = screen.queryAllByText(/%/);
    expect(percentElements).toHaveLength(0);
  });

  it('has hover effect classes', () => {
    const { container } = render(
      <StatCard label="Test" value="1" icon={Zap} color="text-blue-500" />,
    );
    const card = container.firstChild as HTMLElement;
    expect(card.className).toContain('hover:shadow-md');
    expect(card.className).toContain('hover:-translate-y-0.5');
  });

  it('renders with all stat card example configs', () => {
    const examples = [
      { label: 'Edits/sec', value: '7.3', icon: Zap, color: 'text-blue-500', accentColor: 'border-blue-500' },
      { label: 'Edits Today', value: '234,567', icon: Activity, color: 'text-green-500', accentColor: 'border-green-500' },
      { label: 'Hot Pages', value: '145', icon: TrendingUp, color: 'text-orange-500', accentColor: 'border-orange-500' },
      { label: 'Active Alerts', value: '12', icon: AlertTriangle, color: 'text-red-500', accentColor: 'border-red-500' },
    ];

    for (const example of examples) {
      const { unmount } = render(<StatCard {...example} />);
      expect(screen.getByText(example.label)).toBeInTheDocument();
      expect(screen.getByText(example.value)).toBeInTheDocument();
      unmount();
    }
  });
});
