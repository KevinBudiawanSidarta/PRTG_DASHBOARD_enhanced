'use client';
import { useCallback, useEffect, useState } from 'react';

const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
const ORG = process.env.NEXT_PUBLIC_ORGANIZATION_ID || '11111111-1111-1111-1111-111111111111';
const money = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 });

async function apiFetch<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API}${path}`, {
    ...opts,
    headers: { 'Content-Type': 'application/json', 'X-Organization-ID': ORG, ...opts.headers },
    cache: 'no-store',
  });
  if (!res.ok) throw new Error(await res.text());
  return res.status === 204 ? ({} as T) : res.json();
}

type KBEntry = {
  id: string;
  device_pattern: string;
  sensor_pattern: string;
  service_category: string;
  description: string;
  hourly_loss_estimate: number;
  affected_users_estimate: number;
  affected_processes: string[];
  sla_penalty_per_hour: number;
  recovery_time_estimate_minutes: number;
  recovery_procedure: string;
  priority: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
};

const EMPTY_FORM = {
  device_pattern: '',
  sensor_pattern: '',
  service_category: 'network',
  description: '',
  hourly_loss_estimate: '0',
  affected_users_estimate: '0',
  affected_processes: '',
  sla_penalty_per_hour: '0',
  recovery_time_estimate_minutes: '60',
  recovery_procedure: '',
  priority: 'P2',
  is_active: true,
};

const CATEGORIES = [
  { value: 'network', label: 'Network' },
  { value: 'server', label: 'Server' },
  { value: 'application', label: 'Application' },
  { value: 'database', label: 'Database' },
  { value: 'storage', label: 'Storage' },
  { value: 'security', label: 'Security' },
];

export default function KnowledgeBase() {
  const [entries, setEntries] = useState<KBEntry[]>([]);
  const [form, setForm] = useState({ ...EMPTY_FORM });
  const [editing, setEditing] = useState<KBEntry | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [msg, setMsg] = useState('');
  const [msgType, setMsgType] = useState<'success' | 'error'>('success');
  const [loading, setLoading] = useState(true);
  const [filterCategory, setFilterCategory] = useState('all');
  const [filterPriority, setFilterPriority] = useState('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [expandedEntry, setExpandedEntry] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      const data = await apiFetch<{ items: KBEntry[] }>('/api/v1/knowledge-base');
      setEntries(data.items || []);
    } catch { /* ignore */ } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  function setF(key: string, value: string | boolean) {
    setForm(f => ({ ...f, [key]: value }));
  }

  function notify(text: string, type: 'success' | 'error' = 'success') {
    setMsg(text);
    setMsgType(type);
    setTimeout(() => setMsg(''), 5000);
  }

  function startEdit(entry?: KBEntry) {
    if (entry) {
      setEditing(entry);
      setForm({
        device_pattern: entry.device_pattern,
        sensor_pattern: entry.sensor_pattern || '',
        service_category: entry.service_category,
        description: entry.description,
        hourly_loss_estimate: String(entry.hourly_loss_estimate),
        affected_users_estimate: String(entry.affected_users_estimate),
        affected_processes: (entry.affected_processes || []).join(', '),
        sla_penalty_per_hour: String(entry.sla_penalty_per_hour),
        recovery_time_estimate_minutes: String(entry.recovery_time_estimate_minutes),
        recovery_procedure: entry.recovery_procedure || '',
        priority: entry.priority,
        is_active: entry.is_active,
      });
    } else {
      setEditing(null);
      setForm({ ...EMPTY_FORM });
    }
    setShowForm(true);
    setMsg('');
  }

  async function save() {
    if (!form.device_pattern.trim()) {
      notify('Device pattern wajib diisi', 'error');
      return;
    }
    if (!form.description.trim()) {
      notify('Deskripsi dampak wajib diisi', 'error');
      return;
    }
    const payload = {
      device_pattern: form.device_pattern.trim(),
      sensor_pattern: form.sensor_pattern.trim(),
      service_category: form.service_category,
      description: form.description.trim(),
      hourly_loss_estimate: Number(form.hourly_loss_estimate) || 0,
      affected_users_estimate: Number(form.affected_users_estimate) || 0,
      affected_processes: form.affected_processes
        .split(',')
        .map(s => s.trim())
        .filter(Boolean),
      sla_penalty_per_hour: Number(form.sla_penalty_per_hour) || 0,
      recovery_time_estimate_minutes: Number(form.recovery_time_estimate_minutes) || 60,
      recovery_procedure: form.recovery_procedure.trim(),
      priority: form.priority,
      is_active: form.is_active,
    };

    try {
      if (editing) {
        await apiFetch(`/api/v1/knowledge-base/${editing.id}`, {
          method: 'PUT',
          body: JSON.stringify(payload),
        });
        notify('Knowledge base entry berhasil diperbarui');
      } else {
        await apiFetch('/api/v1/knowledge-base', {
          method: 'POST',
          body: JSON.stringify(payload),
        });
        notify('Knowledge base entry berhasil dibuat');
      }
      setShowForm(false);
      setEditing(null);
      setForm({ ...EMPTY_FORM });
      await load();
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Gagal menyimpan', 'error');
    }
  }

  async function remove(id: string) {
    if (!confirm('Hapus knowledge base entry ini?')) return;
    try {
      await apiFetch(`/api/v1/knowledge-base/${id}`, { method: 'DELETE' });
      notify('Entry berhasil dihapus');
      await load();
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Gagal menghapus', 'error');
    }
  }

  // Filtering
  const filtered = entries.filter(e => {
    if (filterCategory !== 'all' && e.service_category !== filterCategory) return false;
    if (filterPriority !== 'all' && e.priority !== filterPriority) return false;
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      return (
        e.device_pattern.toLowerCase().includes(q) ||
        e.description.toLowerCase().includes(q) ||
        (e.sensor_pattern || '').toLowerCase().includes(q)
      );
    }
    return true;
  });

  // Stats
  const totalLoss = entries.reduce((s, e) => s + (e.is_active ? e.hourly_loss_estimate : 0), 0);
  const activeCount = entries.filter(e => e.is_active).length;
  const byCategory = CATEGORIES.map(c => ({
    ...c,
    count: entries.filter(e => e.service_category === c.value).length,
  }));

  return (
    <>
      <div className="page-header">
        <div className="page-eyebrow">Knowledge Base Management</div>
        <h1 className="page-title">Business Impact Knowledge Base</h1>
        <p className="page-sub">
          Kelola aturan dampak bisnis untuk setiap device dan sensor. Data ini digunakan oleh AI Analysis engine untuk menghitung estimasi kerugian.
        </p>
      </div>

      {/* KPIs */}
      <div className="kpi-grid" style={{ gridTemplateColumns: 'repeat(4,1fr)', marginBottom: 20 }}>
        <div className="kpi-card" id="kpi-kb-total">
          <div className="kpi-header">
            <span className="kpi-label">Total Entries</span>
          </div>
          <div className="kpi-value">{entries.length}</div>
          <div className="kpi-meta">{activeCount} aktif</div>
        </div>
        <div className="kpi-card" id="kpi-kb-loss">
          <div className="kpi-header">
            <span className="kpi-label">Max Exposure/Jam</span>
          </div>
          <div className="kpi-value" style={{ fontSize: totalLoss > 999999999 ? 20 : 28 }}>
            {money.format(totalLoss)}
          </div>
          <div className="kpi-meta">Total semua entry aktif</div>
        </div>
        <div className="kpi-card" id="kpi-kb-categories">
          <div className="kpi-header">
            <span className="kpi-label">Categories</span>
          </div>
          <div className="kpi-value">{byCategory.filter(c => c.count > 0).length}</div>
          <div className="kpi-meta">Dari {CATEGORIES.length} kategori</div>
        </div>
        <div className="kpi-card" id="kpi-kb-p1">
          <div className="kpi-header">
            <span className="kpi-label">Critical (P1)</span>
          </div>
          <div className="kpi-value">{entries.filter(e => e.priority === 'P1').length}</div>
          <div className="kpi-meta">Prioritas tertinggi</div>
        </div>
      </div>

      {/* Category Chips */}
      <div className="kb-category-chips">
        {byCategory.map(c => (
          <div
            key={c.value}
            className={`kb-cat-chip ${filterCategory === c.value ? 'active' : ''}`}
            onClick={() => setFilterCategory(filterCategory === c.value ? 'all' : c.value)}
          >
            <span>{c.label}</span>
            <span className="kb-cat-count">{c.count}</span>
          </div>
        ))}
      </div>

      {/* Controls Bar */}
      <div className="panel" id="kb-controls-panel" style={{ marginBottom: 16 }}>
        <div className="panel-header">
          <div style={{ display: 'flex', gap: 12, alignItems: 'center', flex: 1 }}>
            <input
              type="text"
              className="form-input"
              placeholder="Cari device, sensor, atau deskripsi..."
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              style={{ maxWidth: 320 }}
              id="kb-search-input"
            />
            <select
              className="form-select"
              value={filterPriority}
              onChange={e => setFilterPriority(e.target.value)}
              style={{ width: 140 }}
              id="kb-priority-filter"
            >
              <option value="all">All Priority</option>
              <option value="P1">P1 - Critical</option>
              <option value="P2">P2 - High</option>
              <option value="P3">P3 - Medium</option>
              <option value="P4">P4 - Low</option>
            </select>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{filtered.length} entries</span>
          </div>
          <button className="btn primary" onClick={() => startEdit()} id="new-kb-btn">
            + New Entry
          </button>
        </div>
      </div>

      {/* Toast */}
      {msg && (
        <div className={`toast ${msgType === 'error' ? 'error' : ''}`} style={{ marginBottom: 16 }}>
          {msgType === 'success' ? '✓' : '✕'} {msg}
        </div>
      )}

      {/* Form (Create/Edit) */}
      {showForm && (
        <div className="panel kb-form-panel" id="kb-form-panel" style={{ marginBottom: 16 }}>
          <div className="panel-header">
            <div>
              <div className="panel-title">{editing ? 'Edit Knowledge Base Entry' : '+ New Knowledge Base Entry'}</div>
              <div className="panel-sub">{editing ? `Editing: ${editing.device_pattern}` : 'Definisikan aturan dampak bisnis baru'}</div>
            </div>
            <button className="btn sm" onClick={() => { setShowForm(false); setEditing(null); }}>Cancel</button>
          </div>
          <div className="profile-form">
            <div className="form-grid three" style={{ marginBottom: 14 }}>
              {/* Device Pattern */}
              <div className="form-group">
                <label className="form-label">Device Pattern *</label>
                <input
                  className="form-input" value={form.device_pattern}
                  onChange={e => setF('device_pattern', e.target.value)}
                  placeholder="e.g., Core-Switch-01"
                  id="kb-device-pattern"
                />
                <small style={{ fontSize: 10, color: 'var(--text-muted)' }}>Substring match ke nama device PRTG</small>
              </div>
              {/* Sensor Pattern */}
              <div className="form-group">
                <label className="form-label">Sensor Pattern</label>
                <input
                  className="form-input" value={form.sensor_pattern}
                  onChange={e => setF('sensor_pattern', e.target.value)}
                  placeholder="e.g., Ping, Traffic"
                  id="kb-sensor-pattern"
                />
                <small style={{ fontSize: 10, color: 'var(--text-muted)' }}>Optional — kosongkan untuk semua sensor</small>
              </div>
              {/* Service Category */}
              <div className="form-group">
                <label className="form-label">Kategori *</label>
                <select className="form-select" value={form.service_category} onChange={e => setF('service_category', e.target.value)} id="kb-category-select">
                  {CATEGORIES.map(c => <option key={c.value} value={c.value}>{c.label}</option>)}
                </select>
              </div>
              {/* Priority */}
              <div className="form-group">
                <label className="form-label">Priority *</label>
                <select className="form-select" value={form.priority} onChange={e => setF('priority', e.target.value)} id="kb-priority-select">
                  <option value="P1">P1 - Critical</option>
                  <option value="P2">P2 - High</option>
                  <option value="P3">P3 - Medium</option>
                  <option value="P4">P4 - Low</option>
                </select>
              </div>
              {/* Hourly Loss */}
              <div className="form-group">
                <label className="form-label">Kerugian / Jam (IDR) *</label>
                <input
                  type="number" className="form-input" value={form.hourly_loss_estimate}
                  onChange={e => setF('hourly_loss_estimate', e.target.value)}
                  id="kb-hourly-loss"
                />
              </div>
              {/* SLA Penalty */}
              <div className="form-group">
                <label className="form-label">SLA Penalty / Jam (IDR)</label>
                <input
                  type="number" className="form-input" value={form.sla_penalty_per_hour}
                  onChange={e => setF('sla_penalty_per_hour', e.target.value)}
                  id="kb-sla-penalty"
                />
              </div>
              {/* Affected Users */}
              <div className="form-group">
                <label className="form-label">Affected Users</label>
                <input
                  type="number" className="form-input" value={form.affected_users_estimate}
                  onChange={e => setF('affected_users_estimate', e.target.value)}
                  id="kb-affected-users"
                />
              </div>
              {/* Recovery Time */}
              <div className="form-group">
                <label className="form-label">Recovery Time (menit)</label>
                <input
                  type="number" className="form-input" value={form.recovery_time_estimate_minutes}
                  onChange={e => setF('recovery_time_estimate_minutes', e.target.value)}
                  id="kb-recovery-time"
                />
              </div>
              {/* Active */}
              <div className="form-group" style={{ justifyContent: 'flex-end' }}>
                <label className="kb-toggle-label">
                  <input
                    type="checkbox" checked={form.is_active as boolean}
                    onChange={e => setF('is_active', e.target.checked)}
                    id="kb-is-active"
                  />
                  <span>Aktif</span>
                </label>
              </div>
            </div>

            {/* Full-width fields */}
            <div className="form-group" style={{ marginBottom: 14 }}>
              <label className="form-label">Deskripsi Dampak Bisnis *</label>
              <textarea
                className="form-input" value={form.description}
                onChange={e => setF('description', e.target.value)}
                placeholder="Jelaskan dampak bisnis jika device ini down..."
                rows={3}
                style={{ resize: 'vertical' }}
                id="kb-description"
              />
            </div>
            <div className="form-grid" style={{ marginBottom: 14 }}>
              <div className="form-group">
                <label className="form-label">Proses Bisnis Terdampak</label>
                <input
                  className="form-input" value={form.affected_processes}
                  onChange={e => setF('affected_processes', e.target.value)}
                  placeholder="e.g., Payment Processing, User Authentication (pisahkan koma)"
                  id="kb-affected-processes"
                />
                <small style={{ fontSize: 10, color: 'var(--text-muted)' }}>Pisahkan dengan koma</small>
              </div>
              <div className="form-group">
                <label className="form-label">Recovery Procedure</label>
                <textarea
                  className="form-input" value={form.recovery_procedure}
                  onChange={e => setF('recovery_procedure', e.target.value)}
                  placeholder="Langkah-langkah recovery..."
                  rows={3}
                  style={{ resize: 'vertical' }}
                  id="kb-recovery-procedure"
                />
              </div>
            </div>

            <div style={{ display: 'flex', gap: 8 }}>
              <button className="btn primary" onClick={save} id="save-kb-btn">
                {editing ? 'Update Entry' : '+ Create Entry'}
              </button>
              <button className="btn" onClick={() => { setShowForm(false); setEditing(null); }}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Entries Table */}
      <div className="panel" id="kb-entries-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">Knowledge Base Entries</div>
            <div className="panel-sub">{filtered.length} entries{searchQuery && ` matching "${searchQuery}"`}</div>
          </div>
        </div>

        {loading ? (
          <div style={{ padding: 20 }}>
            {[...Array(4)].map((_, i) => (
              <div key={i} style={{ height: 60, borderRadius: 'var(--radius-xs)', marginBottom: 8 }} className="skeleton" />
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <div className="empty-state" style={{ padding: 64 }}>
            <p>
              {entries.length === 0
                ? 'Belum ada knowledge base entry. Klik "+ New Entry" untuk mulai.'
                : 'Tidak ada entry yang cocok dengan filter.'}
            </p>
          </div>
        ) : (
          <div className="kb-entries-list">
            {filtered.map(entry => {
              const cat = CATEGORIES.find(c => c.value === entry.service_category);
              const isExpanded = expandedEntry === entry.id;
              return (
                <div className={`kb-entry-row ${!entry.is_active ? 'inactive' : ''}`} key={entry.id} id={`kb-entry-${entry.id}`}>
                  <div className="kb-entry-main" onClick={() => setExpandedEntry(isExpanded ? null : entry.id)}>
                    <div className="kb-entry-left">
                      <div>
                        <div className="kb-entry-device">{entry.device_pattern}</div>
                        <div className="kb-entry-desc">{entry.description}</div>
                        <div className="kb-entry-sensor">
                          {cat?.label || entry.service_category}{entry.sensor_pattern && ` · Sensor: ${entry.sensor_pattern}`}
                        </div>
                      </div>
                    </div>
                    <div className="kb-entry-right">
                      <div className="kb-entry-metrics">
                        <span className="kb-metric-value">{money.format(entry.hourly_loss_estimate)}</span>
                        <span className="kb-metric-label">/ jam</span>
                      </div>
                      <span className={`pill ${entry.priority.toLowerCase() === 'p1' ? 'critical' : entry.priority.toLowerCase() === 'p2' ? 'major' : entry.priority.toLowerCase() === 'p3' ? 'minor' : 'info'}`}>
                        {entry.priority}
                      </span>
                      {!entry.is_active && <span className="pill versioned">INACTIVE</span>}
                    </div>
                  </div>

                  {isExpanded && (
                    <div className="kb-entry-expanded">
                      <div className="kb-expanded-grid">
                        <div className="kb-expanded-item">
                          <span className="kb-expanded-label">SLA Penalty/Jam</span>
                          <span className="kb-expanded-value">{money.format(entry.sla_penalty_per_hour)}</span>
                        </div>
                        <div className="kb-expanded-item">
                          <span className="kb-expanded-label">Affected Users</span>
                          <span className="kb-expanded-value">{entry.affected_users_estimate.toLocaleString()}</span>
                        </div>
                        <div className="kb-expanded-item">
                          <span className="kb-expanded-label">Recovery Time</span>
                          <span className="kb-expanded-value">{entry.recovery_time_estimate_minutes} menit</span>
                        </div>
                        <div className="kb-expanded-item">
                          <span className="kb-expanded-label">Category</span>
                          <span className="kb-expanded-value">{cat?.label || entry.service_category}</span>
                        </div>
                      </div>
                      {entry.affected_processes && entry.affected_processes.length > 0 && (
                        <div style={{ marginTop: 12 }}>
                          <span className="kb-expanded-label" style={{ marginBottom: 6, display: 'block' }}>Proses Terdampak</span>
                          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                            {entry.affected_processes.map((p, i) => (
                              <span className="ai-process-tag" key={i}>{p}</span>
                            ))}
                          </div>
                        </div>
                      )}
                      {entry.recovery_procedure && (
                        <div style={{ marginTop: 12 }}>
                          <span className="kb-expanded-label" style={{ marginBottom: 6, display: 'block' }}>Recovery Procedure</span>
                          <pre className="ai-recovery-text">{entry.recovery_procedure}</pre>
                        </div>
                      )}
                      <div className="kb-entry-actions">
                        <button className="btn sm" onClick={() => startEdit(entry)} id={`edit-kb-${entry.id}`}>Edit</button>
                        <button className="btn sm danger" onClick={() => remove(entry.id)} id={`delete-kb-${entry.id}`}>Delete</button>
                      </div>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </>
  );
}
