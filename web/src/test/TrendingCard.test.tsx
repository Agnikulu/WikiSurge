import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { TrendingCard } from '../components/Trending/TrendingCard';
import type { TrendingPage } from '../types';

function makePage(overrides: Partial<TrendingPage> = {}): TrendingPage {
  return {
    title: 'Climate change',
    score: 245.7,
    edits_1h: 34,
    last_edit: new Date().toISOString(),
    rank: 1,
    language: 'en',
    ...overrides,
  };
}

describe('TrendingCard', () => {
  it('renders page title', () => {
    render(<TrendingCard page={makePage()} rank={1} />);
    expect(screen.getByText('Climate change')).toBeInTheDocument();
  });

  it('displays rank number with # prefix', () => {
    render(<TrendingCard page={makePage()} rank={5} />);
    expect(screen.getByText('#5')).toBeInTheDocument();
  });

  it('displays formatted score', () => {
    render(<TrendingCard page={makePage({ score: 245.7 })} rank={1} />);
    expect(screen.getByText('245.7')).toBeInTheDocument();
  });

  it('displays large score in k format', () => {
    render(<TrendingCard page={makePage({ score: 1500 })} rank={1} />);
    expect(screen.getByText('1.5k')).toBeInTheDocument();
  });

  it('displays edit count', () => {
    render(<TrendingCard page={makePage({ edits_1h: 34 })} rank={1} />);
    expect(screen.getByText('34 edits/hr')).toBeInTheDocument();
    expect(screen.getByText('34 edits')).toBeInTheDocument();
  });

  it('renders language flag emoji for en', () => {
    render(<TrendingCard page={makePage({ language: 'en' })} rank={1} />);
    expect(screen.getByText('🇬🇧')).toBeInTheDocument();
  });

  it('renders language code for unknown languages', () => {
    render(<TrendingCard page={makePage({ language: 'xy' })} rank={1} />);
    expect(screen.getByText('XY')).toBeInTheDocument();
  });

  it('renders page title as a clickable link', () => {
    render(<TrendingCard page={makePage()} rank={1} />);
    const link = screen.getByRole('link', { name: /Climate change/ });
    expect(link).toHaveAttribute('href');
    expect(link.getAttribute('href')).toContain('wikipedia.org');
    expect(link).toHaveAttribute('target', '_blank');
  });

  // Top 3 special styling
  it('applies gold styling for rank 1', () => {
    const { container } = render(<TrendingCard page={makePage()} rank={1} />);
    const badge = container.querySelector('[class*="from-yellow"]');
    expect(badge).not.toBeNull();
  });

  it('applies silver styling for rank 2', () => {
    const { container } = render(<TrendingCard page={makePage()} rank={2} />);
    const badge = container.querySelector('[class*="from-gray"]');
    expect(badge).not.toBeNull();
  });

  it('applies bronze styling for rank 3', () => {
    const { container } = render(<TrendingCard page={makePage()} rank={3} />);
    const badge = container.querySelector('[class*="from-orange"]');
    expect(badge).not.toBeNull();
  });

  it('applies plain styling for rank > 3', () => {
    const { container } = render(<TrendingCard page={makePage()} rank={10} />);
    const badge = container.querySelector('[class*="bg-gray-100"]');
    expect(badge).not.toBeNull();
  });

  it('top 3 ranks have larger badge size', () => {
    const { container } = render(<TrendingCard page={makePage()} rank={1} />);
    const badge = container.querySelector('[class*="w-10"]');
    expect(badge).not.toBeNull();
  });

  it('shows new entry highlight when isNew is true', () => {
    const { container } = render(<TrendingCard page={makePage()} rank={4} isNew />);
    const wrapper = container.firstChild as HTMLElement;
    expect(wrapper.className).toContain('animate-slide-up');
    expect(wrapper.className).toContain('ring-1');
  });

  it('shows trending up indicator when rank improved', () => {
    render(<TrendingCard page={makePage()} rank={3} previousRank={7} />);
    const arrow = screen.getByLabelText('up 4');
    expect(arrow).toBeInTheDocument();
  });

  it('shows trending down indicator when rank dropped', () => {
    render(<TrendingCard page={makePage()} rank={7} previousRank={3} />);
    const arrow = screen.getByLabelText('down 4');
    expect(arrow).toBeInTheDocument();
  });

  it('truncates long titles', () => {
    const longTitle = 'A'.repeat(100);
    render(<TrendingCard page={makePage({ title: longTitle })} rank={1} />);
    const link = screen.getByRole('link');
    expect(link.textContent!.length).toBeLessThan(100);
    expect(link.textContent!).toContain('…');
  });
});
