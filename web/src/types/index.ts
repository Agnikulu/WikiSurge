// WikiSurge Data Types

export interface Edit {
  id: string;
  title: string;
  user: string;
  wiki: string;
  byte_change: number;
  timestamp: string;
  bot: boolean;
  comment: string;
}

export interface TrendingPage {
  title: string;
  score: number;
  edits_1h: number;
  last_edit: string;
  rank: number;
  language: string;
}

export interface SpikeAlert {
  type: 'spike';
  page_title: string;
  spike_ratio: number;
  severity: 'low' | 'medium' | 'high' | 'critical';
  timestamp: string;
  edits_5min: number;
}

export interface EditWarAlert {
  type: 'edit_war';
  page_title: string;
  editor_count: number;
  edit_count: number;
  revert_count: number;
  severity: string;
  start_time: string;
}

export interface Stats {
  edits_per_second: number;
  edits_today: number;
  hot_pages_count: number;
  trending_count: number;
  active_alerts: number;
}

export type Alert = SpikeAlert | EditWarAlert;

export interface EditWar {
  page_title: string;
  editors: string[];
  edit_count: number;
  revert_count: number;
  severity: string;
  start_time: string;
  last_edit: string;
  active: boolean;
}

export interface SearchResult {
  edits: Edit[];
  total: number;
}

export interface WebSocketMessage<T = unknown> {
  type: string;
  data: T;
  timestamp: string;
}

export interface PaginationParams {
  limit: number;
  offset?: number;
}

export interface FilterState {
  languages: string[];
  excludeBots: boolean;
  minByteChange: number;
}
