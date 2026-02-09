import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AlertCard } from '../components/Alerts/AlertCard';
import type { SpikeAlert, EditWarAlert } from '../types';

const spikeAlert: SpikeAlert = {
  type: 'spike',
  page_title: 'Breaking news article',
  spike_ratio: 8.3,
  severity: 'critical',
  timestamp: new Date().toISOString(),
  edits_5min: 47,
};

const editWarAlert: EditWarAlert = {
  type: 'edit_war',
  page_title: 'Controversial topic',
  editor_count: 5,
  edit_count: 23,
  revert_count: 12,
  severity: 'high',
  start_time: new Date().toISOString(),
};

describe('AlertCard – Spike Alert', () => {
  it('renders page title', () => {
    render(<AlertCard alert={spikeAlert} />);
    expect(screen.getByText('Breaking news article')).toBeInTheDocument();
  });

  it('displays Spike Detected header', () => {
    render(<AlertCard alert={spikeAlert} />);
    expect(screen.getByText('Spike Detected')).toBeInTheDocument();
  });

  it('shows spike ratio', () => {
    render(<AlertCard alert={spikeAlert} />);
    expect(screen.getByText('8.3x normal rate')).toBeInTheDocument();
  });

  it('shows edit count', () => {
    render(<AlertCard alert={spikeAlert} />);
    expect(screen.getByText('47 edits in 5 min')).toBeInTheDocument();
  });

  it('shows severity badge', () => {
    render(<AlertCard alert={spikeAlert} />);
    expect(screen.getByText('critical')).toBeInTheDocument();
  });

  it('uses 🔴 icon for critical spike', () => {
    render(<AlertCard alert={spikeAlert} />);
    expect(screen.getByLabelText('spike')).toHaveTextContent('🔴');
  });

  it('uses ⚠️ icon for non-critical spike', () => {
    const alert: SpikeAlert = { ...spikeAlert, severity: 'medium' };
    render(<AlertCard alert={alert} />);
    expect(screen.getByLabelText('spike')).toHaveTextContent('⚠️');
  });

  it('applies red border for critical severity', () => {
    const { container } = render(<AlertCard alert={spikeAlert} />);
    const card = container.querySelector('[class*="border-red"]');
    expect(card).not.toBeNull();
  });

  it('renders title as clickable link', () => {
    render(<AlertCard alert={spikeAlert} />);
    const link = screen.getByRole('link', { name: /Breaking news article/ });
    expect(link).toHaveAttribute('href');
    expect(link.getAttribute('href')).toContain('wikipedia.org');
  });
});

describe('AlertCard – Edit War Alert', () => {
  it('renders page title', () => {
    render(<AlertCard alert={editWarAlert} />);
    expect(screen.getByText('Controversial topic')).toBeInTheDocument();
  });

  it('displays Edit War header', () => {
    render(<AlertCard alert={editWarAlert} />);
    expect(screen.getByText('Edit War')).toBeInTheDocument();
  });

  it('shows editor count', () => {
    render(<AlertCard alert={editWarAlert} />);
    expect(screen.getByText('5 editors')).toBeInTheDocument();
  });

  it('shows revert count', () => {
    render(<AlertCard alert={editWarAlert} />);
    expect(screen.getByText('12 reverts')).toBeInTheDocument();
  });

  it('shows edit count', () => {
    render(<AlertCard alert={editWarAlert} />);
    expect(screen.getByText('23 edits')).toBeInTheDocument();
  });

  it('shows severity badge', () => {
    render(<AlertCard alert={editWarAlert} />);
    expect(screen.getByText('high')).toBeInTheDocument();
  });

  it('shows ⚔️ icon', () => {
    render(<AlertCard alert={editWarAlert} />);
    expect(screen.getByLabelText('edit war')).toHaveTextContent('⚔️');
  });

  it('applies orange border for high severity', () => {
    const { container } = render(<AlertCard alert={editWarAlert} />);
    const card = container.querySelector('[class*="border-orange"]');
    expect(card).not.toBeNull();
  });
});

describe('AlertCard – Dismiss', () => {
  it('calls onDismiss when dismiss button clicked', async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    render(<AlertCard alert={spikeAlert} onDismiss={onDismiss} />);

    const btn = screen.getByLabelText('Dismiss alert');
    await user.click(btn);
    expect(onDismiss).toHaveBeenCalledWith(spikeAlert);
  });

  it('does not render dismiss button when no handler', () => {
    render(<AlertCard alert={spikeAlert} />);
    expect(screen.queryByLabelText('Dismiss alert')).not.toBeInTheDocument();
  });
});
