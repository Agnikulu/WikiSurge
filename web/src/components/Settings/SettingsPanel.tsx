import { useState, useCallback, useEffect } from 'react';
import {
  Settings,
  Mail,
  Eye,
  Star,
  Save,
  Loader2,
  LogOut,
  CheckCircle,
  X,
  Plus,
  Trash2,
} from 'lucide-react';
import { useAuthStore } from '../../store/authStore';
import type { DigestFrequency, DigestContent, DigestPreferences } from '../../types/user';

export function SettingsPanel() {
  const { user, updatePreferences, updateWatchlist, logout, isLoading, error, clearError } =
    useAuthStore();

  const [frequency, setFrequency] = useState<DigestFrequency>(user?.digest_frequency ?? 'none');
  const [content, setContent] = useState<DigestContent>(user?.digest_content ?? 'both');
  const [threshold, setThreshold] = useState(user?.spike_threshold ?? 3.0);
  const [watchlist, setWatchlist] = useState<string[]>(user?.watchlist ?? []);
  const [newPage, setNewPage] = useState('');
  const [saved, setSaved] = useState(false);
  const [watchlistSaved, setWatchlistSaved] = useState(false);

  // Sync from store if user changes
  useEffect(() => {
    if (user) {
      setFrequency(user.digest_frequency);
      setContent(user.digest_content);
      setThreshold(user.spike_threshold);
      setWatchlist(user.watchlist ?? []);
    }
  }, [user]);

  const handleSavePreferences = useCallback(async () => {
    clearError();
    const prefs: DigestPreferences = {
      digest_frequency: frequency,
      digest_content: content,
      spike_threshold: threshold,
    };
    try {
      await updatePreferences(prefs);
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
    } catch {
      // error in store
    }
  }, [frequency, content, threshold, updatePreferences, clearError]);

  const handleAddPage = useCallback(() => {
    const title = newPage.trim();
    if (!title || watchlist.includes(title)) return;
    if (watchlist.length >= 100) return;
    setWatchlist([...watchlist, title]);
    setNewPage('');
  }, [newPage, watchlist]);

  const handleRemovePage = useCallback(
    (title: string) => {
      setWatchlist(watchlist.filter((p) => p !== title));
    },
    [watchlist]
  );

  const handleSaveWatchlist = useCallback(async () => {
    clearError();
    try {
      await updateWatchlist(watchlist);
      setWatchlistSaved(true);
      setTimeout(() => setWatchlistSaved(false), 2500);
    } catch {
      // error in store
    }
  }, [watchlist, updateWatchlist, clearError]);

  if (!user) return null;

  return (
    <div className="max-w-2xl mx-auto space-y-6">
      {/* Account header */}
      <div
        className="rounded-xl p-5"
        style={{
          background: 'rgba(13,21,37,0.95)',
          border: '1px solid rgba(0,255,136,0.15)',
        }}
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div
              className="w-10 h-10 rounded-full flex items-center justify-center text-sm font-bold font-mono"
              style={{ background: 'rgba(0,255,136,0.15)', color: '#00ff88' }}
            >
              {user.email[0].toUpperCase()}
            </div>
            <div>
              <p className="text-sm font-mono font-semibold" style={{ color: '#e2e8f0' }}>
                {user.email}
              </p>
              <p className="text-[10px] font-mono" style={{ color: 'rgba(0,255,136,0.4)' }}>
                ID: {user.id.slice(0, 8)}...
              </p>
            </div>
          </div>
          <button
            onClick={logout}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-mono transition-colors"
            style={{
              background: 'rgba(255,68,68,0.1)',
              border: '1px solid rgba(255,68,68,0.2)',
              color: '#ff6666',
            }}
          >
            <LogOut className="h-3.5 w-3.5" />
            SIGN OUT
          </button>
        </div>
      </div>

      {/* Error */}
      {error && (
        <div
          className="flex items-center gap-2 px-3 py-2 rounded-lg text-xs font-mono"
          style={{
            background: 'rgba(255,68,68,0.1)',
            border: '1px solid rgba(255,68,68,0.25)',
            color: '#ff6666',
          }}
        >
          {error}
          <button onClick={clearError} className="ml-auto">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}

      {/* Digest Preferences */}
      <div
        className="rounded-xl p-5"
        style={{
          background: 'rgba(13,21,37,0.95)',
          border: '1px solid rgba(0,255,136,0.15)',
        }}
      >
        <h3 className="flex items-center gap-2 text-sm font-mono font-bold mb-4" style={{ color: '#00ff88' }}>
          <Mail className="h-4 w-4" />
          DIGEST EMAIL PREFERENCES
        </h3>

        {/* Frequency */}
        <div className="mb-5">
          <label className="block text-xs font-mono mb-2" style={{ color: 'rgba(0,255,136,0.6)' }}>
            <Settings className="h-3 w-3 inline mr-1" />
            FREQUENCY
          </label>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
            {(['none', 'daily', 'weekly', 'both'] as DigestFrequency[]).map((f) => (
              <button
                key={f}
                onClick={() => setFrequency(f)}
                className="px-3 py-2 rounded-lg text-xs font-mono font-medium transition-all"
                style={{
                  background: frequency === f ? 'rgba(0,255,136,0.15)' : 'rgba(0,255,136,0.03)',
                  border: `1px solid ${frequency === f ? 'rgba(0,255,136,0.4)' : 'rgba(0,255,136,0.1)'}`,
                  color: frequency === f ? '#00ff88' : 'rgba(0,255,136,0.5)',
                }}
              >
                {f.toUpperCase()}
              </button>
            ))}
          </div>
          <p className="text-[10px] font-mono mt-1.5" style={{ color: 'rgba(0,255,136,0.3)' }}>
            {frequency === 'none' && 'No digest emails'}
            {frequency === 'daily' && 'Receive a digest every day at 8:00 UTC'}
            {frequency === 'weekly' && 'Receive a digest every Monday at 8:00 UTC'}
            {frequency === 'both' && 'Receive daily + weekly summary emails'}
          </p>
        </div>

        {/* Content type */}
        <div className="mb-5">
          <label className="block text-xs font-mono mb-2" style={{ color: 'rgba(0,255,136,0.6)' }}>
            <Eye className="h-3 w-3 inline mr-1" />
            CONTENT
          </label>
          <div className="grid grid-cols-3 gap-2">
            {(['both', 'watchlist', 'global'] as DigestContent[]).map((c) => (
              <button
                key={c}
                onClick={() => setContent(c)}
                className="px-3 py-2 rounded-lg text-xs font-mono font-medium transition-all"
                style={{
                  background: content === c ? 'rgba(0,255,136,0.15)' : 'rgba(0,255,136,0.03)',
                  border: `1px solid ${content === c ? 'rgba(0,255,136,0.4)' : 'rgba(0,255,136,0.1)'}`,
                  color: content === c ? '#00ff88' : 'rgba(0,255,136,0.5)',
                }}
              >
                {c === 'both' ? 'ALL' : c.toUpperCase()}
              </button>
            ))}
          </div>
        </div>

        {/* Spike threshold */}
        <div className="mb-5">
          <label className="block text-xs font-mono mb-2" style={{ color: 'rgba(0,255,136,0.6)' }}>
            SPIKE THRESHOLD: {threshold.toFixed(1)}x
          </label>
          <input
            type="range"
            min="1"
            max="20"
            step="0.5"
            value={threshold}
            onChange={(e) => setThreshold(parseFloat(e.target.value))}
            className="w-full accent-green-400"
          />
          <div className="flex justify-between text-[10px] font-mono" style={{ color: 'rgba(0,255,136,0.3)' }}>
            <span>1x (all activity)</span>
            <span>20x (major only)</span>
          </div>
        </div>

        {/* Save button */}
        <button
          onClick={handleSavePreferences}
          disabled={isLoading}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-mono font-bold transition-all disabled:opacity-50"
          style={{
            background: saved ? 'rgba(0,255,136,0.2)' : 'rgba(0,255,136,0.12)',
            border: '1px solid rgba(0,255,136,0.3)',
            color: '#00ff88',
          }}
        >
          {isLoading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : saved ? (
            <CheckCircle className="h-3.5 w-3.5" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {saved ? 'SAVED' : 'SAVE PREFERENCES'}
        </button>
      </div>

      {/* Watchlist */}
      <div
        className="rounded-xl p-5"
        style={{
          background: 'rgba(13,21,37,0.95)',
          border: '1px solid rgba(0,255,136,0.15)',
        }}
      >
        <h3 className="flex items-center gap-2 text-sm font-mono font-bold mb-4" style={{ color: '#00ff88' }}>
          <Star className="h-4 w-4" />
          WATCHLIST
          <span
            className="ml-auto text-[10px] font-normal px-2 py-0.5 rounded-full"
            style={{ background: 'rgba(0,255,136,0.1)', color: 'rgba(0,255,136,0.5)' }}
          >
            {watchlist.length}/100
          </span>
        </h3>

        {/* Add page */}
        <div className="flex gap-2 mb-4">
          <input
            type="text"
            value={newPage}
            onChange={(e) => setNewPage(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleAddPage()}
            placeholder="Wikipedia article title (e.g. Bitcoin)"
            className="flex-1 px-3 py-2 rounded-lg text-sm font-mono outline-none"
            style={{
              background: 'rgba(0,255,136,0.05)',
              border: '1px solid rgba(0,255,136,0.2)',
              color: '#e2e8f0',
            }}
          />
          <button
            onClick={handleAddPage}
            disabled={!newPage.trim() || watchlist.length >= 100}
            className="px-3 py-2 rounded-lg text-xs font-mono font-bold transition-all disabled:opacity-30"
            style={{
              background: 'rgba(0,255,136,0.12)',
              border: '1px solid rgba(0,255,136,0.3)',
              color: '#00ff88',
            }}
          >
            <Plus className="h-4 w-4" />
          </button>
        </div>

        {/* Page list */}
        {watchlist.length === 0 ? (
          <p className="text-xs font-mono text-center py-4" style={{ color: 'rgba(0,255,136,0.3)' }}>
            No pages in your watchlist yet. Add Wikipedia article titles above.
          </p>
        ) : (
          <ul className="space-y-1.5 max-h-64 overflow-y-auto">
            {watchlist.map((page) => (
              <li
                key={page}
                className="flex items-center justify-between px-3 py-2 rounded-lg group"
                style={{
                  background: 'rgba(0,255,136,0.03)',
                  border: '1px solid rgba(0,255,136,0.08)',
                }}
              >
                <span className="text-xs font-mono" style={{ color: '#e2e8f0' }}>
                  {page}
                </span>
                <button
                  onClick={() => handleRemovePage(page)}
                  className="opacity-0 group-hover:opacity-100 transition-opacity"
                  title="Remove from watchlist"
                >
                  <Trash2 className="h-3.5 w-3.5" style={{ color: '#ff6666' }} />
                </button>
              </li>
            ))}
          </ul>
        )}

        {/* Save watchlist */}
        <button
          onClick={handleSaveWatchlist}
          disabled={isLoading}
          className="mt-4 flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-mono font-bold transition-all disabled:opacity-50"
          style={{
            background: watchlistSaved ? 'rgba(0,255,136,0.2)' : 'rgba(0,255,136,0.12)',
            border: '1px solid rgba(0,255,136,0.3)',
            color: '#00ff88',
          }}
        >
          {isLoading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : watchlistSaved ? (
            <CheckCircle className="h-3.5 w-3.5" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {watchlistSaved ? 'SAVED' : 'SAVE WATCHLIST'}
        </button>
      </div>
    </div>
  );
}
