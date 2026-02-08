import { describe, it, expect } from 'vitest';
import {
  formatRelativeTime,
  formatByteChange,
  getByteChangeColor,
  truncateTitle,
  getByteChange,
  extractLanguage,
  isNewPage,
  buildWikiUrl,
  buildDiffUrl,
  buildUserUrl,
} from '../utils/formatting';
import type { Edit } from '../types';

describe('formatRelativeTime', () => {
  it('formats a recent unix timestamp', () => {
    const now = Math.floor(Date.now() / 1000);
    expect(formatRelativeTime(now - 5)).toBe('5s ago');
  });

  it('formats minutes', () => {
    const now = Math.floor(Date.now() / 1000);
    expect(formatRelativeTime(now - 120)).toBe('2m ago');
  });

  it('formats hours', () => {
    const now = Math.floor(Date.now() / 1000);
    expect(formatRelativeTime(now - 7200)).toBe('2h ago');
  });

  it('formats days', () => {
    const now = Math.floor(Date.now() / 1000);
    expect(formatRelativeTime(now - 86400 * 3)).toBe('3d ago');
  });

  it('handles ISO string timestamps', () => {
    const date = new Date(Date.now() - 10000);
    expect(formatRelativeTime(date.toISOString())).toBe('10s ago');
  });

  it('returns just now for future timestamps', () => {
    const future = Math.floor(Date.now() / 1000) + 100;
    expect(formatRelativeTime(future)).toBe('just now');
  });
});

describe('formatByteChange', () => {
  it('formats positive bytes', () => {
    expect(formatByteChange(200)).toBe('+200');
  });

  it('formats negative bytes', () => {
    expect(formatByteChange(-300)).toBe('-300');
  });

  it('formats zero', () => {
    expect(formatByteChange(0)).toBe('+0');
  });

  it('formats thousands', () => {
    expect(formatByteChange(1500)).toBe('+1.5K');
  });

  it('formats millions', () => {
    expect(formatByteChange(2500000)).toBe('+2.5M');
  });
});

describe('getByteChangeColor', () => {
  it('returns green for positive', () => {
    expect(getByteChangeColor(100)).toBe('text-green-600');
  });

  it('returns red for negative', () => {
    expect(getByteChangeColor(-100)).toBe('text-red-600');
  });

  it('returns gray for zero', () => {
    expect(getByteChangeColor(0)).toBe('text-gray-500');
  });
});

describe('truncateTitle', () => {
  it('returns short strings unchanged', () => {
    expect(truncateTitle('Hello', 10)).toBe('Hello');
  });

  it('truncates and adds ellipsis for long strings', () => {
    const long = 'A'.repeat(100);
    const result = truncateTitle(long, 50);
    expect(result.length).toBe(51); // 50 chars + ellipsis
    expect(result.endsWith('…')).toBe(true);
  });
});

describe('getByteChange', () => {
  it('computes from length field', () => {
    const edit: Edit = {
      id: 1, title: 'T', user: 'U', wiki: 'enwiki', bot: false,
      timestamp: 0, comment: '',
      length: { old: 100, new: 250 },
    };
    expect(getByteChange(edit)).toBe(150);
  });

  it('prefers byte_change if present', () => {
    const edit: Edit = {
      id: 1, title: 'T', user: 'U', wiki: 'enwiki', bot: false,
      timestamp: 0, comment: '',
      byte_change: 42,
      length: { old: 100, new: 200 },
    };
    expect(getByteChange(edit)).toBe(42);
  });

  it('handles missing length', () => {
    const edit: Edit = {
      id: 1, title: 'T', user: 'U', wiki: 'enwiki', bot: false,
      timestamp: 0, comment: '',
    };
    expect(getByteChange(edit)).toBe(0);
  });
});

describe('extractLanguage', () => {
  it('extracts from enwiki', () => {
    expect(extractLanguage('enwiki')).toBe('en');
  });

  it('extracts from eswiki', () => {
    expect(extractLanguage('eswiki')).toBe('es');
  });

  it('handles empty string', () => {
    expect(extractLanguage('')).toBe('');
  });

  it('handles single char', () => {
    expect(extractLanguage('x')).toBe('x');
  });
});

describe('isNewPage', () => {
  it('returns true when old length is 0', () => {
    const edit: Edit = {
      id: 1, title: 'T', user: 'U', wiki: 'enwiki', bot: false,
      timestamp: 0, comment: '',
      length: { old: 0, new: 500 },
    };
    expect(isNewPage(edit)).toBe(true);
  });

  it('returns false for regular edit', () => {
    const edit: Edit = {
      id: 1, title: 'T', user: 'U', wiki: 'enwiki', bot: false,
      timestamp: 0, comment: '',
      length: { old: 100, new: 200 },
    };
    expect(isNewPage(edit)).toBe(false);
  });

  it('returns false when no length field', () => {
    const edit: Edit = {
      id: 1, title: 'T', user: 'U', wiki: 'enwiki', bot: false,
      timestamp: 0, comment: '',
    };
    expect(isNewPage(edit)).toBe(false);
  });
});

describe('buildWikiUrl', () => {
  it('builds correct URL', () => {
    const url = buildWikiUrl('Climate change', 'https://en.wikipedia.org');
    expect(url).toBe('https://en.wikipedia.org/wiki/Climate_change');
  });

  it('encodes special characters', () => {
    const url = buildWikiUrl('C++ (language)', 'https://en.wikipedia.org');
    expect(url).toContain('/wiki/C%2B%2B');
  });
});

describe('buildDiffUrl', () => {
  it('builds correct diff URL', () => {
    const url = buildDiffUrl(100, 101, 'https://en.wikipedia.org');
    expect(url).toBe('https://en.wikipedia.org/w/index.php?diff=101&oldid=100');
  });
});

describe('buildUserUrl', () => {
  it('builds correct user URL', () => {
    const url = buildUserUrl('JohnDoe', 'https://en.wikipedia.org');
    expect(url).toBe('https://en.wikipedia.org/wiki/User:JohnDoe');
  });
});
