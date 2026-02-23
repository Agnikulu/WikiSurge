import { useEffect, useState, useCallback } from 'react';
import { Shield, Users, Mail, CheckCircle, XCircle, Loader2, RefreshCw, Trash2 } from 'lucide-react';
import { useAuthStore } from '../../store/authStore';

export function AdminPanel() {
  const { user, adminUsers, adminUsersLoading, fetchAdminUsers, deleteAdminUser } = useAuthStore();
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    if (user?.is_admin) {
      fetchAdminUsers();
    }
  }, [user?.is_admin, fetchAdminUsers]);

  const handleDelete = useCallback(async (userId: string) => {
    setDeleting(true);
    try {
      await deleteAdminUser(userId);
    } catch {
      // error shown via store
    } finally {
      setDeleting(false);
      setConfirmDeleteId(null);
    }
  }, [deleteAdminUser]);

  if (!user?.is_admin) return null;

  return (
    <div
      className="rounded-xl p-5"
      style={{
        background: 'rgba(13,21,37,0.95)',
        border: '1px solid rgba(255,170,0,0.25)',
        boxShadow: '0 0 20px rgba(255,170,0,0.05)',
      }}
    >
      {/* Header */}
      <div className="flex items-center justify-between mb-5">
        <h3
          className="flex items-center gap-2 text-sm font-mono font-bold"
          style={{ color: '#ffaa00' }}
        >
          <Shield className="h-4 w-4" />
          ADMIN PANEL
        </h3>
        <button
          onClick={fetchAdminUsers}
          disabled={adminUsersLoading}
          className="flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-[10px] font-mono transition-colors"
          style={{
            background: 'rgba(255,170,0,0.08)',
            border: '1px solid rgba(255,170,0,0.2)',
            color: '#ffaa00',
          }}
          title="Refresh user list"
        >
          <RefreshCw className={`h-3 w-3 ${adminUsersLoading ? 'animate-spin' : ''}`} />
          REFRESH
        </button>
      </div>

      {/* Stats summary */}
      <div className="grid grid-cols-3 gap-3 mb-5">
        <StatCard
          label="TOTAL USERS"
          value={adminUsers?.length ?? 0}
          icon={<Users className="h-3.5 w-3.5" />}
          color="#ffaa00"
        />
        <StatCard
          label="VERIFIED"
          value={adminUsers?.filter((u) => u.verified).length ?? 0}
          icon={<CheckCircle className="h-3.5 w-3.5" />}
          color="#00ff88"
        />
        <StatCard
          label="DIGEST ACTIVE"
          value={
            adminUsers?.filter((u) => u.digest_frequency !== 'none').length ?? 0
          }
          icon={<Mail className="h-3.5 w-3.5" />}
          color="#00aaff"
        />
      </div>

      {/* User list */}
      {adminUsersLoading && !adminUsers ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="h-5 w-5 animate-spin" style={{ color: '#ffaa00' }} />
          <span className="ml-2 text-xs font-mono" style={{ color: 'rgba(255,170,0,0.6)' }}>
            Loading users...
          </span>
        </div>
      ) : adminUsers && adminUsers.length > 0 ? (
        <div className="space-y-2 max-h-[400px] overflow-y-auto pr-1">
          {/* Column headers */}
          <div
            className="grid grid-cols-12 gap-2 px-3 py-1.5 text-[10px] font-mono font-bold sticky top-0 z-10"
            style={{
              color: 'rgba(255,170,0,0.5)',
              background: 'rgba(13,21,37,0.98)',
              borderBottom: '1px solid rgba(255,170,0,0.1)',
            }}
          >
            <span className="col-span-3">EMAIL</span>
            <span className="col-span-2 text-center">STATUS</span>
            <span className="col-span-2 text-center">DIGEST</span>
            <span className="col-span-2 text-center">WATCHLIST</span>
            <span className="col-span-2 text-center">ROLE</span>
            <span className="col-span-1 text-center"></span>
          </div>

          {adminUsers.map((u) => (
            <div
              key={u.id}
              className="grid grid-cols-12 gap-2 items-center px-3 py-2.5 rounded-lg transition-colors"
              style={{
                background: u.is_admin
                  ? 'rgba(255,170,0,0.05)'
                  : 'rgba(0,255,136,0.02)',
                border: `1px solid ${u.is_admin ? 'rgba(255,170,0,0.12)' : 'rgba(0,255,136,0.06)'}`,
              }}
            >
              {/* Email */}
              <div className="col-span-3 min-w-0">
                <p
                  className="text-xs font-mono truncate"
                  style={{ color: '#e2e8f0' }}
                  title={u.email}
                >
                  {u.email}
                </p>
                <p
                  className="text-[9px] font-mono"
                  style={{ color: 'rgba(226,232,240,0.3)' }}
                >
                  {u.id.slice(0, 8)}...
                </p>
              </div>

              {/* Verified status */}
              <div className="col-span-2 flex justify-center">
                {u.verified ? (
                  <span
                    className="flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-mono"
                    style={{
                      background: 'rgba(0,255,136,0.1)',
                      color: '#00ff88',
                    }}
                  >
                    <CheckCircle className="h-2.5 w-2.5" />
                    YES
                  </span>
                ) : (
                  <span
                    className="flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-mono"
                    style={{
                      background: 'rgba(255,68,68,0.1)',
                      color: '#ff6666',
                    }}
                  >
                    <XCircle className="h-2.5 w-2.5" />
                    NO
                  </span>
                )}
              </div>

              {/* Digest frequency */}
              <div className="col-span-2 flex justify-center">
                <span
                  className="px-2 py-0.5 rounded-full text-[10px] font-mono"
                  style={{
                    background:
                      u.digest_frequency === 'none'
                        ? 'rgba(100,100,100,0.15)'
                        : 'rgba(0,170,255,0.1)',
                    color:
                      u.digest_frequency === 'none'
                        ? 'rgba(226,232,240,0.35)'
                        : '#00aaff',
                  }}
                >
                  {u.digest_frequency.toUpperCase()}
                </span>
              </div>

              {/* Watchlist count */}
              <div className="col-span-2 flex justify-center">
                <span
                  className="text-xs font-mono"
                  style={{
                    color:
                      u.watchlist.length > 0
                        ? '#e2e8f0'
                        : 'rgba(226,232,240,0.3)',
                  }}
                >
                  {u.watchlist.length} page{u.watchlist.length !== 1 ? 's' : ''}
                </span>
              </div>

              {/* Role badge */}
              <div className="col-span-2 flex justify-center">
                {u.is_admin ? (
                  <span
                    className="flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-mono font-bold"
                    style={{
                      background: 'rgba(255,170,0,0.15)',
                      border: '1px solid rgba(255,170,0,0.3)',
                      color: '#ffaa00',
                    }}
                  >
                    <Shield className="h-2.5 w-2.5" />
                    ADMIN
                  </span>
                ) : (
                  <span
                    className="px-2 py-0.5 rounded-full text-[10px] font-mono"
                    style={{
                      background: 'rgba(0,255,136,0.06)',
                      color: 'rgba(0,255,136,0.5)',
                    }}
                  >
                    USER
                  </span>
                )}
              </div>

              {/* Delete button — hidden for self */}
              <div className="col-span-1 flex justify-center">
                {!u.is_admin && u.id !== user?.id ? (
                  confirmDeleteId === u.id ? (
                    <div className="flex items-center gap-1">
                      <button
                        onClick={() => handleDelete(u.id)}
                        disabled={deleting}
                        className="px-1.5 py-0.5 rounded text-[9px] font-mono font-bold transition-colors"
                        style={{
                          background: 'rgba(255,68,68,0.2)',
                          border: '1px solid rgba(255,68,68,0.4)',
                          color: '#ff4444',
                        }}
                      >
                        {deleting ? '...' : 'YES'}
                      </button>
                      <button
                        onClick={() => setConfirmDeleteId(null)}
                        className="px-1.5 py-0.5 rounded text-[9px] font-mono transition-colors"
                        style={{
                          background: 'rgba(100,100,100,0.15)',
                          color: 'rgba(226,232,240,0.5)',
                        }}
                      >
                        NO
                      </button>
                    </div>
                  ) : (
                    <button
                      onClick={() => setConfirmDeleteId(u.id)}
                      className="p-1 rounded-lg transition-all opacity-40 hover:opacity-100"
                      title="Delete user"
                      style={{ color: '#ff6666' }}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )
                ) : null}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <p
          className="text-xs font-mono text-center py-6"
          style={{ color: 'rgba(255,170,0,0.4)' }}
        >
          No registered users found.
        </p>
      )}
    </div>
  );
}

function StatCard({
  label,
  value,
  icon,
  color,
}: {
  label: string;
  value: number;
  icon: React.ReactNode;
  color: string;
}) {
  return (
    <div
      className="rounded-lg p-3 text-center"
      style={{
        background: `${color}08`,
        border: `1px solid ${color}20`,
      }}
    >
      <div
        className="flex items-center justify-center gap-1.5 mb-1"
        style={{ color: `${color}99` }}
      >
        {icon}
        <span className="text-[9px] font-mono font-bold">{label}</span>
      </div>
      <p className="text-xl font-mono font-bold" style={{ color }}>
        {value}
      </p>
    </div>
  );
}
