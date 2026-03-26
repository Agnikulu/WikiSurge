import { useState, useCallback, type FormEvent } from 'react';
import {
  LogIn,
  UserPlus,
  AlertCircle,
  Loader2,
  Mail,
  BarChart3,
  ChevronDown,
  ChevronUp,
  ExternalLink,
  Sparkles,
  Calendar,
  CalendarDays,
} from 'lucide-react';
import { useAuthStore } from '../../store/authStore';
import { mockDailyDigest, mockWeeklyDigest } from '../../data/mockDigest';
import type { DigestData, GlobalHighlight, WatchlistEvent } from '../../types/digest';

// -------------------------------------------------------------------
// Digest preview helpers (inline to keep LoginForm self-contained)
// -------------------------------------------------------------------

function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(n >= 10_000 ? 0 : 1) + 'K';
  return n.toLocaleString();
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

function formatDateRange(start: string, end: string): string {
  const s = new Date(start);
  const e = new Date(end);
  return `${s.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })} – ${e.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })}`;
}

function severityColor(severity?: string): string {
  switch (severity) {
    case 'critical': return '#ef4444';
    case 'high': return '#f59e0b';
    case 'moderate': return '#3b82f6';
    case 'low': return '#22c55e';
    default: return '#8b949e';
  }
}

function severityBadge(severity?: string): string {
  switch (severity) {
    case 'critical': return '🔴 CRITICAL';
    case 'high': return '🟠 HIGH';
    case 'moderate': return '🟡 MODERATE';
    case 'low': return '🟢 LOW';
    default: return '';
  }
}

function intensityColor(edits: number): string {
  if (edits >= 500) return '#ef4444';
  if (edits >= 200) return '#f59e0b';
  if (edits >= 50) return '#8b5cf6';
  return '#00ff88';
}

function battleIntensity(edits: number): string {
  if (edits >= 500) return '⚠️ ALL-OUT WAR';
  if (edits >= 200) return '🔥 HEATED';
  if (edits >= 50) return '⚡ ACTIVE';
  return '💬 SIMMERING';
}

function rankEmoji(rank: number): string {
  switch (rank) { case 1: return '🥇'; case 2: return '🥈'; case 3: return '🥉'; default: return `#${rank}`; }
}

function articleUrl(title: string, serverUrl?: string): string {
  const base = serverUrl || 'https://en.wikipedia.org';
  return `${base}/wiki/${encodeURIComponent(title.replace(/ /g, '_'))}`;
}

// -------------------------------------------------------------------
// Small digest sub-components
// -------------------------------------------------------------------

function MiniEditWarCard({ item }: { item: GlobalHighlight }) {
  const [expanded, setExpanded] = useState(false);
  const iColor = intensityColor(item.edit_count);
  return (
    <div className="rounded-xl overflow-hidden" style={{ background: '#0d1117', border: '1px solid #30363d' }}>
      <div className="h-[3px]" style={{ background: iColor }} />
      <div className="p-3.5">
        {/* Title row */}
        <div className="flex items-start gap-2.5">
          <span className="text-xl leading-none">{rankEmoji(item.rank)}</span>
          <div className="flex-1 min-w-0">
            <a href={articleUrl(item.title, item.server_url)} target="_blank" rel="noopener noreferrer"
              className="text-sm font-bold hover:underline flex items-center gap-1" style={{ color: '#e6edf3' }}>
              ⚔️ {item.title}
              <ExternalLink className="h-2.5 w-2.5 opacity-40" />
            </a>
          </div>
          <div className="text-right shrink-0">
            {item.edit_count > 0 && (
              <>
                <p className="text-lg font-extrabold font-mono" style={{ color: iColor }}>{formatNumber(item.edit_count)}</p>
                <p className="text-[9px] font-mono uppercase tracking-wider" style={{ color: '#8b949e' }}>edits</p>
              </>
            )}
          </div>
        </div>

        {/* LLM Summary */}
        {item.llm_summary && (
          <div className="mt-2.5 rounded-lg" style={{ background: '#161b22', borderLeft: '3px solid #8b5cf6' }}>
            <div className="px-3 py-2">
              <p className="text-[12px] leading-relaxed" style={{ color: '#c9d1d9' }}>
                {expanded || item.llm_summary.length <= 140 ? item.llm_summary : item.llm_summary.slice(0, 140) + '...'}
              </p>
              {item.llm_summary.length > 140 && (
                <button onClick={() => setExpanded(!expanded)} className="flex items-center gap-1 mt-1 text-[10px] font-mono font-semibold" style={{ color: '#8b5cf6' }}>
                  {expanded ? <>Less <ChevronUp className="h-2.5 w-2.5" /></> : <>More <ChevronDown className="h-2.5 w-2.5" /></>}
                </button>
              )}
            </div>
          </div>
        )}

        {/* Stats pills: editors, reverts, severity, content area */}
        <div className="flex flex-wrap items-center gap-1.5 mt-2.5">
          {item.editor_count && item.editor_count > 0 && (
            <span className="text-[10px] font-semibold px-2 py-1 rounded-lg" style={{ background: '#1c1017', border: '1px solid #3d1f1f', color: '#e6edf3' }}>
              👥 {item.editor_count} editors
            </span>
          )}
          {item.revert_count && item.revert_count > 0 && (
            <span className="text-[10px] font-semibold px-2 py-1 rounded-lg" style={{ background: '#1c1017', border: '1px solid #3d1f1f', color: '#e6edf3' }}>
              🔄 {item.revert_count} reverts
            </span>
          )}
          {item.severity && (
            <span className="text-[10px] font-bold px-2 py-1 rounded-lg" style={{ background: severityColor(item.severity), color: '#0d1117', opacity: 0.9 }}>
              {severityBadge(item.severity)}
            </span>
          )}
          {item.content_area && (
            <span className="text-[10px] italic" style={{ color: '#8b949e' }}>
              📌 {item.content_area.length > 40 ? item.content_area.slice(0, 37) + '...' : item.content_area}
            </span>
          )}
        </div>

        {/* Editors list */}
        {item.editors && item.editors.length > 0 && (
          <p className="text-[10px] mt-2" style={{ color: '#484f58' }}>
            Key participants: <span style={{ color: '#8b949e' }}>
              {item.editors.length <= 4 ? item.editors.join(', ') : item.editors.slice(0, 4).join(', ') + ` +${item.editors.length - 4} more`}
            </span>
          </p>
        )}

        {/* Battle intensity badge */}
        <div className="mt-2">
          <span className="text-[9px] font-bold px-2.5 py-0.5 rounded-full" style={{ background: iColor, color: '#0d1117' }}>
            {battleIntensity(item.edit_count)}
          </span>
        </div>
      </div>
    </div>
  );
}

function MiniTrendingCard({ item }: { item: GlobalHighlight }) {
  const iColor = intensityColor(item.edit_count);
  return (
    <div className="rounded-xl overflow-hidden" style={{ background: '#0d1117', border: '1px solid #30363d' }}>
      <div className="h-[3px]" style={{ background: iColor }} />
      <div className="p-3 flex items-start gap-2.5">
        <span className="text-xl leading-none">{rankEmoji(item.rank)}</span>
        <div className="flex-1 min-w-0">
          <a href={articleUrl(item.title, item.server_url)} target="_blank" rel="noopener noreferrer"
            className="text-sm font-bold hover:underline flex items-center gap-1" style={{ color: '#e6edf3' }}>
            🔥 {item.title}
            <ExternalLink className="h-2.5 w-2.5 opacity-40" />
          </a>
          <p className="text-[11px] mt-0.5 leading-relaxed" style={{ color: '#8b949e' }}>{item.summary}</p>
        </div>
        <div className="text-right shrink-0">
          {item.edit_count > 0 && (
            <>
              <p className="text-base font-extrabold font-mono" style={{ color: iColor }}>{formatNumber(item.edit_count)}</p>
              <p className="text-[9px] font-mono uppercase tracking-wider" style={{ color: '#8b949e' }}>edits</p>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function MiniWatchlistNotable({ item }: { item: WatchlistEvent }) {
  const iColor = intensityColor(item.edit_count);
  const icon = item.event_type === 'edit_war' ? '⚔️' : item.event_type === 'spike' ? '📈' : item.event_type === 'trending' ? '🔥' : '✏️';
  return (
    <div className="rounded-xl overflow-hidden" style={{ background: '#0d1117', borderLeft: `4px solid ${iColor}`, border: `1px solid #30363d`, borderLeftWidth: 4, borderLeftColor: iColor }}>
      <div className="p-3">
        <p className="text-sm font-bold" style={{ color: '#e6edf3' }}>{icon} {item.title}</p>
        <p className="text-[11px] mt-1" style={{ color: '#8b949e' }}>{item.summary}</p>
        {item.edit_count > 0 && (
          <div className="flex items-center gap-2 mt-2">
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full" style={{ background: iColor, color: '#0d1117' }}>
              {formatNumber(item.edit_count)} edits
            </span>
            <span className="text-[10px] font-semibold" style={{ color: iColor }}>
              {battleIntensity(item.edit_count)}
            </span>
          </div>
        )}
      </div>
    </div>
  );
}

function MiniWatchlistQuiet({ item }: { item: WatchlistEvent }) {
  return (
    <div className="flex items-start gap-2 px-1 py-1">
      <div className="mt-1.5 w-1.5 h-1.5 rounded-full shrink-0" style={{ background: '#30363d' }} />
      <p className="text-[12px]" style={{ color: '#8b949e' }}>
        <span style={{ color: '#c9d1d9', fontWeight: 600 }}>{item.title}</span> — {item.summary}
      </p>
    </div>
  );
}

function LanguageBar({ language, percentage }: { language: string; percentage: number }) {
  return (
    <div className="flex items-center gap-2 mb-1.5">
      <span className="text-[10px] font-mono font-bold w-7 text-right" style={{ color: '#e6edf3' }}>{language.toUpperCase()}</span>
      <div className="flex-1 h-2 rounded-full" style={{ background: '#21262d' }}>
        <div className="h-full rounded-full" style={{ width: `${Math.min(percentage * 2.5, 100)}%`, background: 'linear-gradient(90deg, #8b5cf6, #00ff88)' }} />
      </div>
      <span className="text-[10px] font-mono w-10 text-right" style={{ color: '#8b949e' }}>{percentage.toFixed(1)}%</span>
    </div>
  );
}

// -------------------------------------------------------------------
// Digest Preview Section
// -------------------------------------------------------------------

function DigestPreviewSection({ onSignUp }: { onSignUp: () => void }) {
  const [period, setPeriod] = useState<'daily' | 'weekly'>('daily');
  const data: DigestData = period === 'daily' ? mockDailyDigest : mockWeeklyDigest;
  const dateLabel = period === 'daily' ? formatDate(data.period_start) : formatDateRange(data.period_start, data.period_end);
  const notableWatchlist = data.watchlist_events.filter((e) => e.is_notable);
  const quietWatchlist = data.watchlist_events.filter((e) => !e.is_notable);

  return (
    <div className="mt-10">
      {/* Promo header */}
      <div className="text-center mb-6">
        <div className="inline-flex items-center gap-2 px-4 py-1.5 rounded-full mb-4"
          style={{ background: 'rgba(139,92,246,0.1)', border: '1px solid rgba(139,92,246,0.25)' }}>
          <Sparkles className="h-3.5 w-3.5" style={{ color: '#a78bfa' }} />
          <span className="text-[11px] font-mono font-bold tracking-wider" style={{ color: '#a78bfa' }}>PREVIEW</span>
        </div>
        <h3 className="text-2xl sm:text-3xl font-extrabold font-mono" style={{ color: '#e6edf3', letterSpacing: '-0.5px' }}>
          See what you'll get
        </h3>
        <p className="text-sm mt-2 max-w-md mx-auto" style={{ color: '#8b949e' }}>
          Create an account to receive{' '}
          <strong style={{ color: '#00ff88' }}>personalized digest emails</strong>{' '}
          with edit wars, trending pages, and your watchlist — delivered daily or weekly.
        </p>
      </div>

      {/* Period toggle */}
      <div className="flex items-center justify-center gap-4 mb-8">
        <button onClick={() => setPeriod('daily')}
          className="flex items-center gap-1.5 px-4 py-2 rounded-xl text-xs font-mono font-bold transition-all"
          style={{ background: period === 'daily' ? 'rgba(0,255,136,0.12)' : 'rgba(0,255,136,0.02)', border: `1px solid ${period === 'daily' ? 'rgba(0,255,136,0.4)' : 'rgba(0,255,136,0.1)'}`, color: period === 'daily' ? '#00ff88' : 'rgba(0,255,136,0.4)' }}>
          <Calendar className="h-3.5 w-3.5" /> DAILY
        </button>
        <button onClick={() => setPeriod('weekly')}
          className="flex items-center gap-1.5 px-4 py-2 rounded-xl text-xs font-mono font-bold transition-all"
          style={{ background: period === 'weekly' ? 'rgba(139,92,246,0.12)' : 'rgba(139,92,246,0.02)', border: `1px solid ${period === 'weekly' ? 'rgba(139,92,246,0.4)' : 'rgba(139,92,246,0.1)'}`, color: period === 'weekly' ? '#a78bfa' : 'rgba(139,92,246,0.4)' }}>
          <CalendarDays className="h-3.5 w-3.5" /> WEEKLY
        </button>
      </div>

      {/* Digest card — wrapped in a "preview" frame */}
      <div className="relative">
        {/* Preview frame */}
        <div className="rounded-2xl overflow-hidden" style={{ background: '#0a0f1a', border: '1px solid rgba(139,92,246,0.15)', boxShadow: '0 8px 40px rgba(0,0,0,0.5)' }}>

          {/* Hero */}
          <div className="rounded-t-2xl overflow-hidden" style={{ background: '#161b22', borderBottom: '1px solid #30363d' }}>
            <div className="h-1" style={{ background: 'linear-gradient(90deg, #00ff88 0%, #8b5cf6 50%, #f59e0b 100%)' }} />
            <div className="text-center py-8 px-5">
              <p className="text-[11px] font-semibold uppercase tracking-[3px]" style={{ color: '#00ff88' }}>Your WikiSurge</p>
              <h2 className="text-3xl sm:text-4xl font-extrabold mt-1.5" style={{ color: '#e6edf3', letterSpacing: '-1px' }}>
                {period === 'daily' ? 'Daily' : 'Weekly'} Digest
              </h2>
              <span className="inline-block text-[11px] font-mono px-3 py-1 rounded-full mt-3" style={{ background: '#21262d', color: '#8b949e' }}>{dateLabel}</span>
              <p className="mt-3 text-[13px]" style={{ color: '#8b949e' }}>
                Here's what happened on Wikipedia {period === 'daily' ? 'yesterday' : 'this week'} ⚡
              </p>
            </div>
          </div>

          {/* Fun Stats */}
          <div className="px-4 py-5" style={{ background: '#161b22', borderBottom: '1px solid #30363d' }}>
            <p className="text-[10px] font-bold uppercase tracking-[3px] text-center mb-4" style={{ color: '#8b949e' }}>📊 Fun Stats</p>
            <div className="text-center mb-4">
              <p className="text-4xl sm:text-5xl font-extrabold font-mono" style={{ color: '#00ff88', letterSpacing: '-2px' }}>{formatNumber(data.stats.total_edits)}</p>
              <p className="text-[11px] font-mono uppercase tracking-widest mt-1" style={{ color: '#8b949e' }}>edits tracked</p>
            </div>
            <div className="grid grid-cols-2 gap-3 mb-4">
              <div className="rounded-xl p-4 text-center" style={{ background: 'rgba(28,16,23,1)', border: '1px solid #3d1f1f' }}>
                <p className="text-2xl font-extrabold font-mono" style={{ color: '#ef4444' }}>{data.stats.edit_wars}</p>
                <p className="text-[10px] font-mono uppercase tracking-widest mt-1" style={{ color: '#8b949e' }}>Edit Wars ⚔️</p>
              </div>
              <div className="rounded-xl p-4 text-center" style={{ background: 'rgba(13,26,45,1)', border: '1px solid #1f3d5c' }}>
                <p className="text-2xl font-extrabold font-mono" style={{ color: '#3b82f6' }}>{data.stats.top_languages[0]?.language.toUpperCase() ?? '—'}</p>
                <p className="text-[10px] font-mono uppercase tracking-widest mt-1" style={{ color: '#8b949e' }}>Top Language 🌍</p>
              </div>
            </div>
            {data.stats.top_languages.length > 1 && (
              <div>
                <p className="text-[10px] font-bold uppercase tracking-widest mb-2" style={{ color: '#8b949e' }}>Language Breakdown</p>
                {data.stats.top_languages.map((lang) => <LanguageBar key={lang.language} language={lang.language} percentage={lang.percentage} />)}
              </div>
            )}
          </div>

          {/* Edit Wars */}
          {data.edit_war_highlights.length > 0 && (
            <div style={{ background: '#161b22', borderBottom: '1px solid #30363d' }}>
              <div className="h-1" style={{ background: 'linear-gradient(90deg, #ef4444 0%, #f59e0b 50%, #ef4444 100%)' }} />
              <div className="px-4 pt-4 pb-1">
                <p className="text-[10px] font-bold uppercase tracking-[3px]" style={{ color: '#ef4444' }}>⚔️ Most Popular Edit Wars</p>
              </div>
              <div className="px-3 pb-4 space-y-2.5">
                {data.edit_war_highlights.slice(0, 3).map((item) => <MiniEditWarCard key={item.title} item={item} />)}
                {data.edit_war_highlights.length > 3 && (
                  <p className="text-center text-[10px] font-mono py-1" style={{ color: '#8b949e' }}>
                    +{data.edit_war_highlights.length - 3} more in your full digest...
                  </p>
                )}
              </div>
            </div>
          )}

          {/* Trending */}
          {data.trending_highlights.length > 0 && (
            <div style={{ background: '#161b22', borderBottom: '1px solid #30363d' }}>
              <div className="h-1" style={{ background: 'linear-gradient(90deg, #f59e0b 0%, #ef4444 50%, #f59e0b 100%)' }} />
              <div className="px-4 pt-4 pb-1">
                <p className="text-[10px] font-bold uppercase tracking-[3px]" style={{ color: '#f59e0b' }}>🔥 Most Trending Pages</p>
              </div>
              <div className="px-3 pb-4 space-y-2.5">
                {data.trending_highlights.slice(0, 3).map((item) => <MiniTrendingCard key={item.title} item={item} />)}
              </div>
            </div>
          )}

          {/* Watchlist */}
          {data.watchlist_events.length > 0 && (
            <div style={{ background: '#161b22', borderBottom: '1px solid #30363d' }}>
              <div className="h-1" style={{ background: 'linear-gradient(90deg, #00ff88 0%, #22c55e 100%)' }} />
              <div className="px-4 pt-4 pb-2">
                <p className="text-[10px] font-bold uppercase tracking-[3px]" style={{ color: '#00ff88' }}>📋 Your Watchlist</p>
              </div>
              <div className="px-3 pb-4">
                {notableWatchlist.length > 0 && (
                  <div className="space-y-2.5">
                    {notableWatchlist.map((item) => <MiniWatchlistNotable key={item.title} item={item} />)}
                  </div>
                )}
                {quietWatchlist.length > 0 && (
                  <div className="mt-3 px-2">
                    <p className="text-[10px] font-semibold uppercase tracking-wider mb-1" style={{ color: '#484f58' }}>Also on your radar</p>
                    {quietWatchlist.map((item) => <MiniWatchlistQuiet key={item.title} item={item} />)}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Footer / CTA */}
          <div className="px-4 py-6 text-center" style={{ background: '#161b22' }}>
            <div className="flex items-center justify-center gap-2 mb-3">
              <BarChart3 className="h-4 w-4" style={{ color: '#00ff88' }} />
              <span className="text-[11px] font-mono font-bold" style={{ color: '#00ff88' }}>WIKISURGE</span>
            </div>
            <p className="text-[12px] font-mono mb-4" style={{ color: '#8b949e' }}>
              Get this delivered to your inbox. <strong style={{ color: '#00ff88' }}>Free.</strong>
            </p>
            <button
              onClick={onSignUp}
              className="inline-flex items-center gap-2 px-6 py-2.5 rounded-xl text-sm font-mono font-bold transition-all"
              style={{
                background: 'rgba(0,255,136,0.15)',
                border: '1px solid rgba(0,255,136,0.4)',
                color: '#00ff88',
              }}
            >
              <UserPlus className="h-4 w-4" />
              CREATE ACCOUNT
            </button>
          </div>
        </div>

        {/* Email icon badge */}
        <div className="absolute -top-4 left-1/2 -translate-x-1/2">
          <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-full"
            style={{ background: '#161b22', border: '1px solid rgba(139,92,246,0.3)', boxShadow: '0 4px 12px rgba(0,0,0,0.3)' }}>
            <Mail className="h-3.5 w-3.5" style={{ color: '#a78bfa' }} />
            <span className="text-[10px] font-mono font-bold" style={{ color: '#a78bfa' }}>DIGEST EMAIL PREVIEW</span>
          </div>
        </div>
      </div>
    </div>
  );
}

// -------------------------------------------------------------------
// Main LoginForm
// -------------------------------------------------------------------

export function LoginForm() {
  const [isRegister, setIsRegister] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [localError, setLocalError] = useState('');

  const { login, register, isLoading, error, clearError } = useAuthStore();

  const handleSubmit = useCallback(
    async (e: FormEvent) => {
      e.preventDefault();
      setLocalError('');
      clearError();

      if (!email || !password) {
        setLocalError('Email and password are required');
        return;
      }

      if (isRegister) {
        if (password.length < 8) {
          setLocalError('Password must be at least 8 characters');
          return;
        }
        if (password !== confirmPassword) {
          setLocalError('Passwords do not match');
          return;
        }
      }

      try {
        if (isRegister) {
          await register(email, password);
        } else {
          await login(email, password);
        }
      } catch {
        // Error is set in store
      }
    },
    [email, password, confirmPassword, isRegister, login, register, clearError]
  );

  const scrollToTop = useCallback(() => {
    setIsRegister(true);
    setLocalError('');
    clearError();
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }, [clearError]);

  const displayError = localError || error;

  return (
    <div className="max-w-xl mx-auto mt-8 pb-8">
      {/* Auth form */}
      <div
        className="rounded-xl p-6"
        style={{
          background: 'rgba(13,21,37,0.95)',
          border: '1px solid rgba(0,255,136,0.15)',
          boxShadow: '0 4px 24px rgba(0,0,0,0.5)',
        }}
      >
        {/* Header */}
        <div className="text-center mb-6">
          <div
            className="inline-flex items-center justify-center w-12 h-12 rounded-full mb-3"
            style={{ background: 'rgba(0,255,136,0.1)' }}
          >
            {isRegister ? (
              <UserPlus className="h-6 w-6" style={{ color: '#00ff88' }} />
            ) : (
              <LogIn className="h-6 w-6" style={{ color: '#00ff88' }} />
            )}
          </div>
          <h2 className="text-xl font-bold font-mono" style={{ color: '#00ff88' }}>
            {isRegister ? 'CREATE ACCOUNT' : 'SIGN IN'}
          </h2>
          <p className="text-xs font-mono mt-1" style={{ color: 'rgba(0,255,136,0.4)' }}>
            {isRegister
              ? 'Set up your WikiSurge digest account'
              : 'Access your digest preferences & watchlist'}
          </p>
        </div>

        {/* Error */}
        {displayError && (
          <div
            className="flex items-center gap-2 px-3 py-2 rounded-lg mb-4 text-xs font-mono"
            style={{
              background: 'rgba(255,68,68,0.1)',
              border: '1px solid rgba(255,68,68,0.25)',
              color: '#ff6666',
            }}
          >
            <AlertCircle className="h-4 w-4 flex-shrink-0" />
            {displayError}
          </div>
        )}

        {/* Form */}
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label htmlFor="email" className="block text-xs font-mono mb-1.5" style={{ color: 'rgba(0,255,136,0.6)' }}>EMAIL</label>
            <input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com" autoComplete="email"
              className="w-full px-3 py-2 rounded-lg text-sm font-mono outline-none transition-colors"
              style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.2)', color: '#e2e8f0' }} />
          </div>
          <div>
            <label htmlFor="password" className="block text-xs font-mono mb-1.5" style={{ color: 'rgba(0,255,136,0.6)' }}>PASSWORD</label>
            <input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)}
              placeholder={isRegister ? 'Min 8 characters' : '••••••••'}
              autoComplete={isRegister ? 'new-password' : 'current-password'}
              className="w-full px-3 py-2 rounded-lg text-sm font-mono outline-none transition-colors"
              style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.2)', color: '#e2e8f0' }} />
          </div>
          {isRegister && (
            <div>
              <label htmlFor="confirmPassword" className="block text-xs font-mono mb-1.5" style={{ color: 'rgba(0,255,136,0.6)' }}>CONFIRM PASSWORD</label>
              <input id="confirmPassword" type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)}
                placeholder="Repeat password" autoComplete="new-password"
                className="w-full px-3 py-2 rounded-lg text-sm font-mono outline-none transition-colors"
                style={{ background: 'rgba(0,255,136,0.05)', border: '1px solid rgba(0,255,136,0.2)', color: '#e2e8f0' }} />
            </div>
          )}
          <button type="submit" disabled={isLoading}
            className="w-full flex items-center justify-center gap-2 py-2.5 rounded-lg text-sm font-mono font-bold transition-all disabled:opacity-50"
            style={{ background: 'rgba(0,255,136,0.15)', border: '1px solid rgba(0,255,136,0.3)', color: '#00ff88' }}>
            {isLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : isRegister ? <UserPlus className="h-4 w-4" /> : <LogIn className="h-4 w-4" />}
            {isLoading ? 'PROCESSING...' : isRegister ? 'CREATE ACCOUNT' : 'SIGN IN'}
          </button>
        </form>

        {/* Toggle */}
        <div className="mt-5 text-center">
          <button
            onClick={() => { setIsRegister(!isRegister); setLocalError(''); clearError(); }}
            className="text-xs font-mono transition-colors"
            style={{ color: 'rgba(0,255,136,0.5)' }}
          >
            {isRegister ? 'Already have an account? ' : "Don't have an account? "}
            <span style={{ color: '#00ff88', textDecoration: 'underline' }}>
              {isRegister ? 'Sign in' : 'Create one'}
            </span>
          </button>
        </div>
      </div>

      {/* Digest Preview — promotional section below the form */}
      <DigestPreviewSection onSignUp={scrollToTop} />
    </div>
  );
}
