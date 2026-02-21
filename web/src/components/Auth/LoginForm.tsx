import { useState, useCallback, type FormEvent } from 'react';
import { LogIn, UserPlus, AlertCircle, Loader2 } from 'lucide-react';
import { useAuthStore } from '../../store/authStore';

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

  const displayError = localError || error;

  return (
    <div className="max-w-md mx-auto mt-12">
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
            <label
              htmlFor="email"
              className="block text-xs font-mono mb-1.5"
              style={{ color: 'rgba(0,255,136,0.6)' }}
            >
              EMAIL
            </label>
            <input
              id="email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              autoComplete="email"
              className="w-full px-3 py-2 rounded-lg text-sm font-mono outline-none transition-colors"
              style={{
                background: 'rgba(0,255,136,0.05)',
                border: '1px solid rgba(0,255,136,0.2)',
                color: '#e2e8f0',
              }}
            />
          </div>

          <div>
            <label
              htmlFor="password"
              className="block text-xs font-mono mb-1.5"
              style={{ color: 'rgba(0,255,136,0.6)' }}
            >
              PASSWORD
            </label>
            <input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={isRegister ? 'Min 8 characters' : '••••••••'}
              autoComplete={isRegister ? 'new-password' : 'current-password'}
              className="w-full px-3 py-2 rounded-lg text-sm font-mono outline-none transition-colors"
              style={{
                background: 'rgba(0,255,136,0.05)',
                border: '1px solid rgba(0,255,136,0.2)',
                color: '#e2e8f0',
              }}
            />
          </div>

          {isRegister && (
            <div>
              <label
                htmlFor="confirmPassword"
                className="block text-xs font-mono mb-1.5"
                style={{ color: 'rgba(0,255,136,0.6)' }}
              >
                CONFIRM PASSWORD
              </label>
              <input
                id="confirmPassword"
                type="password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                placeholder="Repeat password"
                autoComplete="new-password"
                className="w-full px-3 py-2 rounded-lg text-sm font-mono outline-none transition-colors"
                style={{
                  background: 'rgba(0,255,136,0.05)',
                  border: '1px solid rgba(0,255,136,0.2)',
                  color: '#e2e8f0',
                }}
              />
            </div>
          )}

          <button
            type="submit"
            disabled={isLoading}
            className="w-full flex items-center justify-center gap-2 py-2.5 rounded-lg text-sm font-mono font-bold transition-all disabled:opacity-50"
            style={{
              background: 'rgba(0,255,136,0.15)',
              border: '1px solid rgba(0,255,136,0.3)',
              color: '#00ff88',
            }}
          >
            {isLoading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : isRegister ? (
              <UserPlus className="h-4 w-4" />
            ) : (
              <LogIn className="h-4 w-4" />
            )}
            {isLoading
              ? 'PROCESSING...'
              : isRegister
                ? 'CREATE ACCOUNT'
                : 'SIGN IN'}
          </button>
        </form>

        {/* Toggle */}
        <div className="mt-5 text-center">
          <button
            onClick={() => {
              setIsRegister(!isRegister);
              setLocalError('');
              clearError();
            }}
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
    </div>
  );
}
