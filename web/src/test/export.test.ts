import { describe, it, expect } from 'vitest';
import { editsToCsv } from '../utils/export';
import type { Edit } from '../types';

const mockEdit: Edit = {
  id: '1',
  title: 'Test Page',
  user: 'TestUser',
  wiki: 'enwiki',
  bot: false,
  timestamp: '2026-01-15T12:00:00Z',
  comment: 'Fixed typo',
  byte_change: 42,
};

describe('editsToCsv', () => {
  it('generates CSV header row', () => {
    const csv = editsToCsv([]);
    expect(csv).toBe('Title,User,Wiki,Bot,Comment,Byte Change,Timestamp');
  });

  it('includes edit data rows', () => {
    const csv = editsToCsv([mockEdit]);
    const lines = csv.split('\n');
    expect(lines).toHaveLength(2);
    expect(lines[1]).toContain('"Test Page"');
    expect(lines[1]).toContain('"TestUser"');
    expect(lines[1]).toContain('enwiki');
    expect(lines[1]).toContain('42');
  });

  it('marks bot edits as Yes', () => {
    const botEdit: Edit = { ...mockEdit, bot: true };
    const csv = editsToCsv([botEdit]);
    expect(csv).toContain('Yes');
  });

  it('escapes double quotes in CSV', () => {
    const edit: Edit = { ...mockEdit, comment: 'Said "hello" there' };
    const csv = editsToCsv([edit]);
    expect(csv).toContain('""hello""');
  });

  it('handles numeric timestamps', () => {
    const edit: Edit = { ...mockEdit, timestamp: 1705320000 };
    const csv = editsToCsv([edit]);
    const lines = csv.split('\n');
    // Should convert to ISO string
    expect(lines[1]).toContain('T');
  });

  it('handles multiple edits', () => {
    const csv = editsToCsv([mockEdit, { ...mockEdit, id: '2', title: 'Second Page' }]);
    const lines = csv.split('\n');
    expect(lines).toHaveLength(3);
  });

  it('uses byte_change field when present', () => {
    const csv = editsToCsv([mockEdit]);
    expect(csv).toContain('42');
  });

  it('computes byte_change from length when byte_change absent', () => {
    const edit: Edit = {
      ...mockEdit,
      byte_change: undefined,
      length: { old: 100, new: 150 },
    };
    const csv = editsToCsv([edit]);
    expect(csv).toContain('50');
  });
});
