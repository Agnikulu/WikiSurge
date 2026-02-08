import { describe, it, expect } from 'vitest';
import { highlightMatches } from '../utils/highlight';

describe('highlightMatches', () => {
  it('returns full text as non-match when query is empty', () => {
    const result = highlightMatches('Hello world', '');
    expect(result).toEqual([{ text: 'Hello world', match: false }]);
  });

  it('returns full text as non-match when query is whitespace', () => {
    const result = highlightMatches('Hello world', '   ');
    expect(result).toEqual([{ text: 'Hello world', match: false }]);
  });

  it('highlights a single match', () => {
    const result = highlightMatches('Updated election results', 'election');
    expect(result).toHaveLength(3);
    expect(result[0]).toEqual({ text: 'Updated ', match: false });
    expect(result[1]).toEqual({ text: 'election', match: true });
    expect(result[2]).toEqual({ text: ' results', match: false });
  });

  it('is case-insensitive', () => {
    const result = highlightMatches('ELECTION results', 'election');
    expect(result.some((s) => s.match && s.text === 'ELECTION')).toBe(true);
  });

  it('highlights multiple occurrences', () => {
    const result = highlightMatches('test this test case test', 'test');
    const matches = result.filter((s) => s.match);
    expect(matches).toHaveLength(3);
  });

  it('handles text that is entirely the match', () => {
    const result = highlightMatches('election', 'election');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ text: 'election', match: true });
  });

  it('handles empty text', () => {
    const result = highlightMatches('', 'query');
    expect(result).toEqual([{ text: '', match: false }]);
  });

  it('escapes regex special characters in query', () => {
    const result = highlightMatches('Price is $100.00 today', '$100.00');
    const matches = result.filter((s) => s.match);
    expect(matches).toHaveLength(1);
    expect(matches[0].text).toBe('$100.00');
  });

  it('does not match when query not in text', () => {
    const result = highlightMatches('Hello world', 'missing');
    expect(result).toEqual([{ text: 'Hello world', match: false }]);
  });
});
