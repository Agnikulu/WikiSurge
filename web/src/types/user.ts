// User & Auth types for WikiSurge digest system

export interface User {
  id: string;
  email: string;
  verified: boolean;
  digest_frequency: DigestFrequency;
  digest_content: DigestContent;
  spike_threshold: number;
  watchlist: string[];
}

export type DigestFrequency = 'none' | 'daily' | 'weekly' | 'both';
export type DigestContent = 'both' | 'watchlist' | 'global';

export interface DigestPreferences {
  digest_frequency: DigestFrequency;
  digest_content: DigestContent;
  spike_threshold: number;
}

export interface AuthResponse {
  token: string;
  expires_at: string;
  user: User;
}

export interface APIError {
  error: string;
  code: string;
  details?: string;
}
