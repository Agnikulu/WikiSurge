// Digest data types — mirrors Go internal/digest types

export interface DigestData {
  period: 'daily' | 'weekly';
  period_start: string;
  period_end: string;
  global_highlights: GlobalHighlight[];
  edit_war_highlights: GlobalHighlight[];
  trending_highlights: GlobalHighlight[];
  watchlist_events: WatchlistEvent[];
  stats: FunStats;
}

export interface GlobalHighlight {
  rank: number;
  title: string;
  edit_count: number;
  event_type: 'spike' | 'edit_war' | 'trending';
  spike_ratio?: number;
  summary: string;
  server_url?: string;
  // Edit war detail fields
  editor_count?: number;
  editors?: string[];
  revert_count?: number;
  severity?: 'low' | 'moderate' | 'high' | 'critical';
  llm_summary?: string;
  content_area?: string;
}

export interface WatchlistEvent {
  title: string;
  edit_count: number;
  is_notable: boolean;
  spike_ratio?: number;
  event_type: 'spike' | 'edit_war' | 'quiet' | 'trending' | 'active';
  summary: string;
}

export interface FunStats {
  total_edits: number;
  edit_wars: number;
  top_languages: LanguageStat[];
}

export interface LanguageStat {
  language: string;
  count: number;
  percentage: number;
}
