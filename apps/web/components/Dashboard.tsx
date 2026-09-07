'use client';
import { useCallback, useEffect, useMemo, useState } from 'react';

// ─── Types ───────────────────────────────────────────────────────────────────
type Summary = { open_incidents: number; critical_incidents: number; total_impact: number; events_24h: number };
type Incident = { id: string; status: string; severity: string; started_at: string; ended_at: string | null; duration_seconds: number; sensor: { device: string; name: string }; service: { name: string; criticality: string }; total_impact: number; model_version: string };
type Detail = Incident & { impact: any };
type Service = { id: string; name: string; criticality: string };
type Sensor = { id: string; prtg_sensor_id: string; device_name: string; sensor_name: string; last_known_state: string };
type Mapping = { sensor_id: string; prtg_sensor_id: string; device_name: string; sensor_name: string; last_known_state: string; dependency_weight: number };
type Profile = { id: string; business_service_id: string | null; service_name: string; hourly_revenue: number; transactions_per_hour: number; avg_transaction_value: number; service_dependency: number; loss_probability: number; operational_cost_per_hour: number; penalty_fixed: number; recovery_fixed: number; valid_from: string; valid_to: string | null; active: boolean };
type TechEvent = { id: string; state: string; device: string; sensor: string; occurred_at: string };

type Tab = 'monitoring' | 'overview' | 'impact' | 'mapping' | 'financial';

// ─── Constants ───────────────────────────────────────────────────────────────
const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
const ORG = process.env.NEXT_PUBLIC_ORGANIZATION_ID || '11111111-1111-1111-1111-111111111111';
const money = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 });

// ─── Helpers ─────────────────────────────────────────────────────────────────
function fmtDuration(s: number) {
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  return h ? `${h}h ${m}m` : m ? `${m}m ${sec}s` : `${sec}s`;
}
function timeAgo(iso: string) {
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  return `${Math.floor(diff / 3600)}h ago`;
}
async function apiFetch<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API}${path}`, {
    ...opts,
    headers: { 'Content-Type': 'application/json', 'X-Organization-ID': ORG, ...opts.headers },
    cache: 'no-store',
  });
  if (!res.ok) throw new Error(await res.text());
  return res.status === 204 ? ({} as T) : res.json();
}

// ─── Root Dashboard ───────────────────────────────────────────────────────────
export default function Dashboard() {
  const [tab, setTab] = useState<Tab>('monitoring');
  const [summary, setSummary] = useState<Summary>({ open_incidents: 0, critical_incidents: 0, total_impact: 0, events_24h: 0 });
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [events, setEvents] = useState<TechEvent[]>([]);
  const [sensors, setSensors] = useState<Sensor[]>([]);
  const [selected, setSelected] = useState<Detail | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [lastRefresh, setLastRefresh] = useState(new Date());

  const refresh = useCallback(async () => {
    try {
      const [s, i, e, sns] = await Promise.all([
        apiFetch<Summary>('/api/v1/dashboard/summary'),
        apiFetch<{ items: Incident[] }>('/api/v1/incidents?limit=50'),
        apiFetch<{ items: TechEvent[] }>('/api/v1/technical-events?limit=20'),
        apiFetch<{ items: Sensor[] }>('/api/v1/sensors'),
      ]);
      setSummary(s);
      setIncidents(i.items);
      setEvents(e.items);
      setSensors(sns.items);
      setError('');
      setLastRefresh(new Date());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'API unavailable');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 15000);
    return () => clearInterval(t);
  }, [refresh]);

  const openIncidents = useMemo(() => incidents.filter(x => x.status === 'OPEN' || x.status === 'ACKNOWLEDGED'), [incidents]);
  const criticalCount = useMemo(() => incidents.filter(x => x.severity === 'CRITICAL' && (x.status === 'OPEN' || x.status === 'ACKNOWLEDGED')).length, [incidents]);
  const sensorDown   = useMemo(() => sensors.filter(s => s.last_known_state === 'down').length, [sensors]);
  const sensorUp     = useMemo(() => sensors.filter(s => s.last_known_state === 'up').length, [sensors]);

  const navItems: { id: Tab; icon: string; label: string; badge?: number }[] = [
    { id: 'monitoring', icon: '📡', label: 'Live Monitoring', badge: sensorDown || undefined },
    { id: 'overview',   icon: '📊', label: 'Executive Overview', badge: openIncidents.length || undefined },
    { id: 'impact',     icon: '💸', label: 'Business Impact' },
    { id: 'mapping',    icon: '🔗', label: 'Service Mapping' },
    { id: 'financial',  icon: '💰', label: 'Financial Profiles' },
  ];

  async function handleIncidentAction(id: string, action: 'ack' | 'close') {
    await apiFetch(`/api/v1/incidents/${id}/${action}`, { method: 'POST' });
    await refresh();
    if (selected?.id === id) setSelected(await apiFetch<Detail>(`/api/v1/incidents/${id}`));
  }

  return (
    <div className="app-shell">
      {/* ── Sidebar ── */}
      <aside className="sidebar">
        <div className="sidebar-brand">
          <div className="brand-icon">⚡</div>
          <div className="brand-text">
            <span className="brand-name">BIA Platform</span>
            <span className="brand-sub">IT Intelligence</span>
          </div>
        </div>

        <div className="sidebar-label">Navigation</div>
        <nav className="sidebar-nav">
          {navItems.map(n => (
            <button
              key={n.id}
              className={`nav-item${tab === n.id ? ' active' : ''}`}
              onClick={() => setTab(n.id)}
              id={`nav-${n.id}`}
            >
              <span className="nav-icon">{n.icon}</span>
              {n.label}
              {!!n.badge && <span className="nav-badge">{n.badge}</span>}
            </button>
          ))}
        </nav>

        <div className="sidebar-footer">
          <div className="live-badge">
            <span className="pulse-dot" />
            Live · 15s refresh
          </div>
          <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 8, padding: '0 4px' }}>
            Last: {lastRefresh.toLocaleTimeString()}
          </div>
        </div>
      </aside>

      {/* ── Main ── */}
      <main className="main-content">
        {error && (
          <div className="alert-banner" id="api-error-banner">
            ⚠️ API connection issue: {error}
          </div>
        )}

        {tab === 'monitoring' && (
          <MonitoringTab sensors={sensors} events={events} loading={loading} onRefresh={refresh} />
        )}
        {tab === 'overview' && (
          <OverviewTab
            summary={{ ...summary, open_incidents: openIncidents.length, critical_incidents: criticalCount }}
            incidents={incidents}
            events={events}
            loading={loading}
            onRefresh={refresh}
            onSelect={async (id) => setSelected(await apiFetch<Detail>(`/api/v1/incidents/${id}`))}
            sensorUp={sensorUp}
            sensorDown={sensorDown}
          />
        )}
        {tab === 'impact' && (
          <ImpactTab incidents={incidents} onSelect={async (id) => setSelected(await apiFetch<Detail>(`/api/v1/incidents/${id}`))} />
        )}
        {tab === 'mapping' && <MappingTab />}
        {tab === 'financial' && <FinancialTab />}
      </main>

      {/* ── Incident Drawer ── */}
      {selected && (
        <div className="overlay" onClick={() => setSelected(null)}>
          <div className="drawer" onClick={e => e.stopPropagation()}>
            <div className="drawer-header">
              <div>
                <div className="drawer-eyebrow">Incident Detail</div>
                <div className="drawer-title">{selected.service?.name || 'Unmapped Service'}</div>
              </div>
              <button className="btn sm" onClick={() => setSelected(null)} id="drawer-close-btn">✕ Close</button>
            </div>

            <div className="drawer-body">
              <div className="detail-hero">
                <span className={`pill ${selected.severity.toLowerCase()}`}>{selected.severity}</span>
                <span className={`pill ${selected.status.toLowerCase()}`}>{selected.status}</span>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                  {selected.sensor?.name} @ {selected.sensor?.device}
                </span>
                <span className="detail-duration">{fmtDuration(selected.duration_seconds)}</span>
              </div>

              {selected.impact ? (
                <>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4 }}>Estimated Financial Loss</div>
                  <div className="impact-big">{money.format(selected.impact.total_impact)}</div>
                  <div className="breakdown-list">
                    {Object.entries(selected.impact.breakdown || {}).map(([k, v]) => (
                      <div className="breakdown-row" key={k}>
                        <span>{k.replaceAll('_', ' ')}</span>
                        <b>{money.format(Number(v))}</b>
                      </div>
                    ))}
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 8 }}>Input Snapshot</div>
                  <pre className="snapshot-code">{JSON.stringify(selected.impact.input_snapshot, null, 2)}</pre>
                </>
              ) : (
                <div className="empty-state">
                  <div className="empty-icon">⏳</div>
                  <p>Financial calculation pending.<br />Available after incident is RESOLVED.</p>
                </div>
              )}
            </div>

            <div className="drawer-actions">
              {selected.status === 'OPEN' && (
                <button className="btn" id={`ack-btn-${selected.id}`} onClick={() => handleIncidentAction(selected.id, 'ack')}>
                  ✓ Acknowledge
                </button>
              )}
              {selected.status === 'ACKNOWLEDGED' && (
                <button className="btn success" id={`close-btn-${selected.id}`} onClick={() => handleIncidentAction(selected.id, 'close')}>
                  ✔ Close Incident
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Tab: Live Monitoring ─────────────────────────────────────────────────────
function MonitoringTab({ sensors, events, loading, onRefresh }: {
  sensors: Sensor[]; events: TechEvent[]; loading: boolean; onRefresh: () => void;
}) {
  const up      = sensors.filter(s => s.last_known_state === 'up').length;
  const down    = sensors.filter(s => s.last_known_state === 'down').length;
  const warning = sensors.filter(s => s.last_known_state === 'warning').length;
  const unknown = sensors.filter(s => !['up','down','warning'].includes(s.last_known_state)).length;

  return (
    <>
      <div className="page-header">
        <div className="page-eyebrow">Live Monitoring</div>
        <h1 className="page-title">PRTG Sensor Status</h1>
        <p className="page-sub">Real-time status dari semua sensor yang terhubung ke PRTG. Auto-refresh setiap 15 detik.</p>
      </div>

      <div className="kpi-grid" style={{ gridTemplateColumns: 'repeat(4,1fr)', marginBottom: 20 }}>
        <KpiCard label="Total Sensors"  value={sensors.length} icon="📡" meta="Terdaftar di sistem" />
        <KpiCard label="Operational"    value={up}      icon="✅" meta="Status UP" cls="success" />
        <KpiCard label="Down"           value={down}    icon="🔴" meta="Perlu perhatian segera" cls={down > 0 ? 'danger' : ''} />
        <KpiCard label="Warning"        value={warning} icon="⚠️" meta="Status degraded" />
      </div>

      <div className="content-grid">
        <div className="panel" id="sensor-grid-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">Sensor Grid</div>
              <div className="panel-sub">{sensors.length} sensors dari PRTG</div>
            </div>
            <button className="btn sm" onClick={onRefresh} id="refresh-sensors-btn">
              {loading ? '⟳ Loading…' : '⟳ Refresh'}
            </button>
          </div>

          {loading && sensors.length === 0 ? (
            <div className="sensor-grid">
              {[...Array(8)].map((_, i) => (
                <div key={i} style={{ height: 110, borderRadius: 'var(--radius-sm)' }} className="skeleton" />
              ))}
            </div>
          ) : sensors.length === 0 ? (
            <div className="empty-state">
              <div className="empty-icon">📡</div>
              <p>Belum ada sensor terdaftar.<br />Tambahkan mapping sensor dari tab Service Mapping.</p>
            </div>
          ) : (
            <div className="sensor-grid">
              {sensors.map(s => (
                <SensorCard key={s.id} sensor={s} />
              ))}
            </div>
          )}
        </div>

        <div className="panel" id="event-feed-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">Event Feed</div>
              <div className="panel-sub">State changes terbaru</div>
            </div>
          </div>
          <div className="event-feed">
            {events.length === 0 ? (
              <div className="empty-state" style={{ padding: 32 }}>
                <div className="empty-icon">📋</div>
                <p>Belum ada event</p>
              </div>
            ) : events.map(e => (
              <div className="event-item" key={e.id}>
                <span className={`event-dot ${e.state}`} />
                <div className="event-body">
                  <div className="event-sensor">{e.sensor}</div>
                  <div className="event-device">{e.device}</div>
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 4 }}>
                  <span className={`status-badge ${e.state}`}>
                    <span className="status-dot" />{e.state}
                  </span>
                  <span className="event-time">{timeAgo(e.occurred_at)}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </>
  );
}

function SensorCard({ sensor }: { sensor: Sensor }) {
  const state = sensor.last_known_state || 'unknown';
  return (
    <div className={`sensor-card ${state}`} id={`sensor-${sensor.id}`}>
      <div className="sensor-status-row">
        <span className={`status-badge ${state}`}>
          <span className="status-dot" />{state.toUpperCase()}
        </span>
        <span className="sensor-prtg-id">#{sensor.prtg_sensor_id}</span>
      </div>
      <div className="sensor-device">{sensor.device_name}</div>
      <div className="sensor-name">{sensor.sensor_name}</div>
      <div className="sensor-footer">Sensor ID: {sensor.prtg_sensor_id}</div>
    </div>
  );
}

// ─── Tab: Executive Overview ──────────────────────────────────────────────────
function OverviewTab({ summary, incidents, events, loading, onRefresh, onSelect, sensorUp, sensorDown }: {
  summary: Summary; incidents: Incident[]; events: TechEvent[]; loading: boolean;
  onRefresh: () => void; onSelect: (id: string) => void; sensorUp: number; sensorDown: number;
}) {
  return (
    <>
      <div className="page-header">
        <div className="page-eyebrow">Executive Overview</div>
        <h1 className="page-title">Operational Dashboard</h1>
        <p className="page-sub">Ringkasan insiden aktif, dampak finansial, dan telemetri PRTG terbaru.</p>
      </div>

      <div className="kpi-grid">
        <KpiCard label="Open Incidents"  value={summary.open_incidents}   icon="🚨" meta="Active & acknowledged" cls={summary.open_incidents > 0 ? 'danger' : ''} />
        <KpiCard label="Critical"        value={summary.critical_incidents} icon="🔴" meta="Requires exec attention" cls={summary.critical_incidents > 0 ? 'danger' : ''} />
        <KpiCard label="30-Day Impact"   value={money.format(summary.total_impact)} icon="💸" meta="Resolved incidents only" />
        <KpiCard label="Events · 24h"    value={summary.events_24h} icon="📈" meta="Raw PRTG telemetry" />
      </div>

      <div className="content-grid">
        <div className="panel" id="incidents-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">Active & Recent Incidents</div>
              <div className="panel-sub">Klik baris untuk melihat detail & kalkulasi dampak</div>
            </div>
            <button className="btn sm" onClick={onRefresh} id="refresh-incidents-btn">
              {loading ? '⟳ Loading…' : '⟳ Refresh'}
            </button>
          </div>
          <div className="table-wrap">
            <table id="incidents-table">
              <thead>
                <tr>
                  <th>Severity</th>
                  <th>Service</th>
                  <th>Sensor</th>
                  <th>Status</th>
                  <th>Duration</th>
                  <th>Impact (IDR)</th>
                </tr>
              </thead>
              <tbody>
                {incidents.length === 0 ? (
                  <tr><td colSpan={6}>
                    <div className="empty-state" style={{ padding: 32 }}>
                      <div className="empty-icon">✅</div>
                      <p>Tidak ada insiden aktif</p>
                    </div>
                  </td></tr>
                ) : incidents.map(inc => (
                  <tr key={inc.id} onClick={() => onSelect(inc.id)} id={`incident-row-${inc.id}`}>
                    <td><span className={`pill ${inc.severity.toLowerCase()}`}>{inc.severity}</span></td>
                    <td>
                      <strong>{inc.service?.name || 'Unmapped'}</strong>
                      <small>{inc.service?.criticality || '—'}</small>
                    </td>
                    <td>
                      <strong>{inc.sensor?.name}</strong>
                      <small>{inc.sensor?.device}</small>
                    </td>
                    <td><span className={`pill ${inc.status.toLowerCase()}`}>{inc.status}</span></td>
                    <td style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 12 }}>{fmtDuration(inc.duration_seconds)}</td>
                    <td style={{ fontWeight: 700, color: inc.total_impact > 0 ? '#fca5a5' : 'var(--text-muted)' }}>
                      {money.format(inc.total_impact)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        <div className="panel" id="events-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">Technical Events</div>
              <div className="panel-sub">Append-only PRTG telemetry</div>
            </div>
          </div>
          <div className="event-feed">
            {events.map(e => (
              <div className="event-item" key={e.id}>
                <span className={`event-dot ${e.state}`} />
                <div className="event-body">
                  <div className="event-sensor">{e.sensor}</div>
                  <div className="event-device">{e.device}</div>
                </div>
                <span className="event-time">{new Date(e.occurred_at).toLocaleTimeString()}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="insight-panel">
        <h3>📌 Cara Baca Dashboard</h3>
        <p>
          Financial impact dikalkulasi berdasarkan financial profile yang aktif saat insiden terjadi dan model dampak yang dipublikasi.
          Perubahan profil membuat versi baru dan mempertahankan asumsi historis agar perhitungan insiden tetap auditable.
          Sensor PRTG dipolling setiap 60 detik; webhook tersedia di <code style={{ fontFamily: 'JetBrains Mono', fontSize: 11, background: 'rgba(255,255,255,0.08)', padding: '1px 6px', borderRadius: 4 }}>/internal/prtg/events</code>.
        </p>
      </div>
    </>
  );
}

// ─── Tab: Business Impact Analysis ───────────────────────────────────────────
function ImpactTab({ incidents, onSelect }: { incidents: Incident[]; onSelect: (id: string) => void }) {
  const resolved = incidents.filter(i => i.status === 'RESOLVED' || i.status === 'CLOSED');
  const open     = incidents.filter(i => i.status === 'OPEN' || i.status === 'ACKNOWLEDGED');
  const totalImpact = resolved.reduce((s, i) => s + (i.total_impact || 0), 0);
  const maxImpact   = Math.max(...resolved.map(i => i.total_impact || 0), 1);

  return (
    <>
      <div className="page-header">
        <div className="page-eyebrow">Business Impact Analysis</div>
        <h1 className="page-title">Financial Loss Intelligence</h1>
        <p className="page-sub">Estimasi kerugian finansial dari setiap insiden berdasarkan profil revenue dan model dampak.</p>
      </div>

      <div className="kpi-grid" style={{ gridTemplateColumns: 'repeat(3,1fr)' }}>
        <KpiCard label="Total Impact (Resolved)" value={money.format(totalImpact)} icon="💸" meta="Kalkulasi final" cls="danger" />
        <KpiCard label="Resolved Incidents"      value={resolved.length}           icon="✅" meta="Sudah ditutup" cls="success" />
        <KpiCard label="Open Incidents"          value={open.length}               icon="🚨" meta="Dampak masih berjalan" cls={open.length > 0 ? 'danger' : ''} />
      </div>

      {resolved.length > 0 && (
        <div className="panel" id="impact-breakdown-panel" style={{ marginBottom: 16 }}>
          <div className="panel-header">
            <div>
              <div className="panel-title">Impact Breakdown per Incident</div>
              <div className="panel-sub">Klik baris untuk detail breakdown finansial</div>
            </div>
          </div>
          <div className="table-wrap">
            <table id="impact-table">
              <thead>
                <tr>
                  <th>Service</th>
                  <th>Sensor</th>
                  <th>Severity</th>
                  <th>Duration</th>
                  <th>Financial Impact</th>
                  <th>Relative</th>
                </tr>
              </thead>
              <tbody>
                {resolved
                  .slice()
                  .sort((a, b) => (b.total_impact || 0) - (a.total_impact || 0))
                  .map(inc => {
                    const pct = Math.round(((inc.total_impact || 0) / maxImpact) * 100);
                    return (
                      <tr key={inc.id} onClick={() => onSelect(inc.id)} id={`impact-row-${inc.id}`}>
                        <td>
                          <strong>{inc.service?.name || 'Unmapped'}</strong>
                          <small>{inc.service?.criticality}</small>
                        </td>
                        <td>
                          <strong>{inc.sensor?.name}</strong>
                          <small>{inc.sensor?.device}</small>
                        </td>
                        <td><span className={`pill ${inc.severity.toLowerCase()}`}>{inc.severity}</span></td>
                        <td style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 12 }}>{fmtDuration(inc.duration_seconds)}</td>
                        <td style={{ fontWeight: 800, color: '#fca5a5' }}>{money.format(inc.total_impact || 0)}</td>
                        <td style={{ minWidth: 140 }}>
                          <div className="impact-bar-wrap">
                            <div className="impact-bar-bg">
                              <div className="impact-bar-fill" style={{ width: `${pct}%` }} />
                            </div>
                            <span style={{ fontSize: 11, color: 'var(--text-muted)', minWidth: 32 }}>{pct}%</span>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {open.length > 0 && (
        <div className="panel" id="open-impact-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">🔴 Active Incidents — Impact Accumulating</div>
              <div className="panel-sub">Kerugian terus bertambah selama insiden berlangsung</div>
            </div>
          </div>
          <div className="table-wrap">
            <table id="open-impact-table">
              <thead>
                <tr><th>Service</th><th>Sensor</th><th>Severity</th><th>Berlangsung</th><th>Status</th></tr>
              </thead>
              <tbody>
                {open.map(inc => (
                  <tr key={inc.id} onClick={() => onSelect(inc.id)} id={`open-impact-row-${inc.id}`}>
                    <td><strong>{inc.service?.name || 'Unmapped'}</strong></td>
                    <td><strong>{inc.sensor?.name}</strong><small>{inc.sensor?.device}</small></td>
                    <td><span className={`pill ${inc.severity.toLowerCase()}`}>{inc.severity}</span></td>
                    <td style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 12, color: '#fca5a5' }}>
                      {fmtDuration(inc.duration_seconds)}
                    </td>
                    <td><span className={`pill ${inc.status.toLowerCase()}`}>{inc.status}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {resolved.length === 0 && open.length === 0 && (
        <div className="panel">
          <div className="empty-state" style={{ padding: 64 }}>
            <div className="empty-icon">📊</div>
            <p>Belum ada data insiden untuk dianalisis.<br />Data akan muncul setelah insiden RESOLVED pertama terjadi.</p>
          </div>
        </div>
      )}
    </>
  );
}

// ─── Tab: Service Mapping ─────────────────────────────────────────────────────
function MappingTab() {
  const [services, setServices]   = useState<Service[]>([]);
  const [sensors, setSensors]     = useState<Sensor[]>([]);
  const [selected, setSelected]   = useState('');
  const [mapped, setMapped]       = useState<Record<string, number>>({});
  const [editing, setEditing]     = useState<Service | null>(null);
  const [name, setName]           = useState('');
  const [criticality, setCriticality] = useState('P2');
  const [newSensor, setNewSensor] = useState('');
  const [weight, setWeight]       = useState('1');
  const [msg, setMsg]             = useState('');
  const [msgType, setMsgType]     = useState<'success' | 'error'>('success');

  const load = useCallback(async () => {
    const [s, ss] = await Promise.all([
      apiFetch<{ items: Service[] }>('/api/v1/services'),
      apiFetch<{ items: Sensor[] }>('/api/v1/sensors'),
    ]);
    setServices(s.items);
    setSensors(ss.items);
    if (!selected && s.items[0]) setSelected(s.items[0].id);
  }, [selected]);

  useEffect(() => { load(); }, [load]);
  useEffect(() => {
    if (!selected) return;
    const svc = services.find(x => x.id === selected);
    if (svc && !editing) { setName(svc.name); setCriticality(svc.criticality); }
    apiFetch<{ items: Mapping[] }>(`/api/v1/services/${selected}/mappings`)
      .then(x => setMapped(Object.fromEntries(x.items.map(m => [m.sensor_id, m.dependency_weight]))))
      .catch(() => setMapped({}));
  }, [selected, services, editing]);

  function resetForm() { setEditing(null); setName(''); setCriticality('P2'); }
  function notify(text: string, type: 'success' | 'error' = 'success') { setMsg(text); setMsgType(type); }

  async function saveService() {
    if (!name) return notify('Nama service tidak boleh kosong', 'error');
    await apiFetch(editing ? `/api/v1/services/${editing.id}` : '/api/v1/services', {
      method: editing ? 'PUT' : 'POST',
      body: JSON.stringify({ name, criticality }),
    });
    notify(editing ? 'Service diperbarui' : 'Service berhasil dibuat');
    resetForm();
    await load();
  }
  async function removeService(id: string) {
    if (!confirm('Hapus service ini dan semua mapping-nya?')) return;
    await apiFetch(`/api/v1/services/${id}`, { method: 'DELETE' });
    if (selected === id) setSelected('');
    notify('Service dihapus');
    await load();
  }
  async function saveMappings() {
    const mappings = Object.entries(mapped).map(([sensor_id, dependency_weight]) => ({ sensor_id, dependency_weight }));
    await apiFetch(`/api/v1/services/${selected}/mappings`, { method: 'PUT', body: JSON.stringify({ mappings }) });
    notify('Mapping berhasil disimpan');
  }
  function addMapping() {
    if (newSensor) setMapped(x => ({ ...x, [newSensor]: Math.min(1, Math.max(0.01, Number(weight) || 1)) }));
    setNewSensor(''); setWeight('1');
  }
  const available = sensors.filter(s => !mapped[s.id]);

  return (
    <>
      <div className="page-header">
        <div className="page-eyebrow">Service Mapping</div>
        <h1 className="page-title">Business Service Mapping</h1>
        <p className="page-sub">Map sensor PRTG ke business service dan tentukan bobot dependency untuk kalkulasi dampak.</p>
      </div>

      <div className="panel" id="mapping-panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">Business Services</div>
            <div className="panel-sub">{services.length} services terdaftar</div>
          </div>
          <button className="btn primary" onClick={() => { setMsg(''); resetForm(); }} id="new-service-btn">+ New Service</button>
        </div>

        <div className="split-layout">
          {/* Left: Service CRUD */}
          <div className="split-left">
            <div className="form-group" style={{ marginBottom: 14 }}>
              <label className="form-label">Business Service</label>
              <select className="form-select" value={selected} onChange={e => setSelected(e.target.value)} id="service-select">
                <option value="">Pilih service…</option>
                {services.map(s => <option key={s.id} value={s.id}>{s.name} · {s.criticality}</option>)}
              </select>
            </div>

            <div className="form-grid" style={{ marginBottom: 14 }}>
              <div className="form-group">
                <label className="form-label">Service Name</label>
                <input className="form-input" value={name} onChange={e => setName(e.target.value)} placeholder="Customer Transaction API" id="service-name-input" />
              </div>
              <div className="form-group">
                <label className="form-label">Criticality</label>
                <select className="form-select" value={criticality} onChange={e => setCriticality(e.target.value)} id="criticality-select">
                  {['P1', 'P2', 'P3', 'P4'].map(x => <option key={x}>{x}</option>)}
                </select>
              </div>
            </div>

            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <button className="btn primary" onClick={saveService} id="save-service-btn">{editing ? '✓ Update' : '+ Create'}</button>
              {selected && !editing && (
                <button className="btn" id="edit-service-btn" onClick={() => {
                  const svc = services.find(x => x.id === selected);
                  if (svc) { setEditing(svc); setName(svc.name); setCriticality(svc.criticality); }
                }}>✎ Edit</button>
              )}
              {editing && <button className="btn" onClick={resetForm}>✕ Cancel</button>}
              {selected && (
                <button className="btn danger" id="delete-service-btn" onClick={() => removeService(selected)}>🗑 Delete</button>
              )}
            </div>

            {msg && <div className={`toast ${msgType === 'error' ? 'error' : ''}`}>{msgType === 'success' ? '✓' : '✕'} {msg}</div>}
          </div>

          {/* Right: Sensor Mapping */}
          <div className="split-right">
            <div className="section-header" style={{ marginBottom: 12 }}>
              <span className="section-title">Mapped Sensors</span>
              <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{Object.keys(mapped).length} sensors</span>
            </div>

            {selected ? (
              <>
                {Object.entries(mapped).map(([sid, w]) => {
                  const s = sensors.find(x => x.id === sid);
                  return (
                    <div className="mapping-row" key={sid}>
                      <div className="sensor-info">
                        <b>{s?.sensor_name || sid}</b>
                        <small>{s?.device_name} · <span className={`status-badge ${s?.last_known_state || 'unknown'}`} style={{ fontSize: 9, padding: '1px 5px' }}>{s?.last_known_state || 'unknown'}</span></small>
                      </div>
                      <input
                        type="number" min="0.01" max="1" step="0.01"
                        className="form-input" value={w}
                        onChange={e => setMapped(m => ({ ...m, [sid]: Number(e.target.value) }))}
                        id={`weight-input-${sid}`}
                      />
                      <button className="btn sm danger" onClick={() => setMapped(m => { const x = { ...m }; delete x[sid]; return x; })}>✕</button>
                    </div>
                  );
                })}

                <div className="add-mapping-row">
                  <select className="form-select" value={newSensor} onChange={e => setNewSensor(e.target.value)} id="add-sensor-select">
                    <option value="">Tambah sensor…</option>
                    {available.map(s => <option key={s.id} value={s.id}>{s.device_name} · {s.sensor_name}</option>)}
                  </select>
                  <input type="number" min=".01" max="1" step=".01" className="form-input" value={weight} onChange={e => setWeight(e.target.value)} id="add-weight-input" />
                  <button className="btn" onClick={addMapping} id="add-mapping-btn">Add</button>
                </div>

                <button className="btn primary" style={{ marginTop: 16 }} onClick={saveMappings} id="save-mappings-btn">
                  💾 Save Mapping
                </button>
              </>
            ) : (
              <div className="empty-state" style={{ padding: 32 }}>
                <div className="empty-icon">🔗</div>
                <p>Pilih atau buat service untuk mengelola mapping sensor.</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </>
  );
}

// ─── Tab: Financial Profiles ──────────────────────────────────────────────────
function FinancialTab() {
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [editing, setEditing]   = useState<Profile | null>(null);
  const [form, setForm] = useState({
    business_service_id: '', hourly_revenue: '200000000',
    transactions_per_hour: '500', avg_transaction_value: '400000',
    service_dependency: '0.85', loss_probability: '0.30',
    operational_cost_per_hour: '1000000', penalty_fixed: '0', recovery_fixed: '0',
  });
  const [msg, setMsg]       = useState('');
  const [msgType, setMsgType] = useState<'success' | 'error'>('success');

  const load = useCallback(async () => {
    const [p, s] = await Promise.all([
      apiFetch<{ items: Profile[] }>('/api/v1/financial-profiles'),
      apiFetch<{ items: Service[] }>('/api/v1/services'),
    ]);
    setProfiles(p.items);
    setServices(s.items);
  }, []);
  useEffect(() => { load(); }, [load]);

  function setF(k: string, v: string) { setForm(f => ({ ...f, [k]: v })); }
  function startEdit(p?: Profile) {
    setEditing(p || null);
    setForm({
      business_service_id: p?.business_service_id || '',
      hourly_revenue: String(p?.hourly_revenue ?? 200000000),
      transactions_per_hour: String(p?.transactions_per_hour ?? 500),
      avg_transaction_value: String(p?.avg_transaction_value ?? 400000),
      service_dependency: String(p?.service_dependency ?? 0.85),
      loss_probability: String(p?.loss_probability ?? 0.30),
      operational_cost_per_hour: String(p?.operational_cost_per_hour ?? 1000000),
      penalty_fixed: String(p?.penalty_fixed ?? 0),
      recovery_fixed: String(p?.recovery_fixed ?? 0),
    });
    setMsg('');
  }
  async function save() {
    const payload = {
      business_service_id: form.business_service_id || null,
      hourly_revenue: Number(form.hourly_revenue),
      transactions_per_hour: Number(form.transactions_per_hour),
      avg_transaction_value: Number(form.avg_transaction_value),
      service_dependency: Number(form.service_dependency),
      loss_probability: Number(form.loss_probability),
      operational_cost_per_hour: Number(form.operational_cost_per_hour),
      penalty_fixed: Number(form.penalty_fixed),
      recovery_fixed: Number(form.recovery_fixed),
    };
    await apiFetch(
      editing ? `/api/v1/financial-profiles/${editing.id}` : '/api/v1/financial-profiles',
      { method: editing ? 'PUT' : 'POST', body: JSON.stringify(payload) }
    );
    setMsg(editing ? 'Profile version dibuat' : 'Profile berhasil dibuat');
    setMsgType('success');
    setEditing(null);
    await load();
  }
  async function remove(id: string) {
    if (!confirm('Deaktivasi financial profile ini?')) return;
    await apiFetch(`/api/v1/financial-profiles/${id}`, { method: 'DELETE' });
    setMsg('Profile dinonaktifkan');
    setMsgType('success');
    await load();
  }

  return (
    <>
      <div className="page-header">
        <div className="page-eyebrow">Financial Profiles</div>
        <h1 className="page-title">Financial Impact Model</h1>
        <p className="page-sub">Kelola asumsi revenue dan kerugian yang digunakan engine kalkulasi dampak insiden.</p>
      </div>

      <div className="panel" id="financial-panel" style={{ marginBottom: 16 }}>
        <div className="panel-header">
          <div>
            <div className="panel-title">Active Financial Profiles</div>
            <div className="panel-sub">Perubahan akan membuat versi baru dan menutup interval sebelumnya (versioned)</div>
          </div>
          <button className="btn primary" onClick={() => startEdit()} id="new-profile-btn">+ New Profile</button>
        </div>
        <div className="table-wrap">
          <table id="profiles-table">
            <thead>
              <tr>
                <th>Service</th>
                <th>Hourly Revenue</th>
                <th>Dependency</th>
                <th>Loss Prob.</th>
                <th>Valid From</th>
                <th>Status</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {profiles.length === 0 ? (
                <tr><td colSpan={7}>
                  <div className="empty-state" style={{ padding: 32 }}>
                    <div className="empty-icon">💰</div>
                    <p>Belum ada financial profile. Buat satu untuk mulai menghitung dampak finansial.</p>
                  </div>
                </td></tr>
              ) : profiles.map(p => (
                <tr key={p.id}>
                  <td>
                    <strong>{p.service_name}</strong>
                    <small>{p.business_service_id || 'org-level default'}</small>
                  </td>
                  <td style={{ fontWeight: 700 }}>{money.format(p.hourly_revenue)}</td>
                  <td>{p.service_dependency.toFixed(2)}</td>
                  <td>{(p.loss_probability * 100).toFixed(0)}%</td>
                  <td style={{ fontSize: 11, color: 'var(--text-muted)' }}>{new Date(p.valid_from).toLocaleDateString('id-ID')}</td>
                  <td>
                    {p.active
                      ? <span className="pill success">ACTIVE</span>
                      : <span className="pill versioned">VERSIONED</span>}
                  </td>
                  <td style={{ textAlign: 'right' }}>
                    {p.active && (
                      <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
                        <button className="btn sm" onClick={() => startEdit(p)} id={`edit-profile-${p.id}`}>✎ Edit</button>
                        <button className="btn sm danger" onClick={() => remove(p.id)} id={`deactivate-profile-${p.id}`}>Deactivate</button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Form */}
      <div className="panel">
        <div className="panel-header">
          <div>
            <div className="panel-title">{editing ? '📝 Create Replacement Version' : '+ New Financial Profile'}</div>
            <div className="panel-sub">{editing ? `Replacing active profile for ${editing.service_name}` : 'Tambah profil keuangan baru'}</div>
          </div>
        </div>
        <div className="profile-form">
          <div className="form-grid three" style={{ marginBottom: 14 }}>
            <div className="form-group" style={{ gridColumn: '1/-1' }}>
              <label className="form-label">Business Service</label>
              <select className="form-select" value={form.business_service_id} onChange={e => setF('business_service_id', e.target.value)} id="profile-service-select">
                <option value="">Organization Default</option>
                {services.map(s => <option key={s.id} value={s.id}>{s.name} · {s.criticality}</option>)}
              </select>
            </div>
            {[
              { key: 'hourly_revenue', label: 'Hourly Revenue (IDR)' },
              { key: 'transactions_per_hour', label: 'Transactions / Hour' },
              { key: 'avg_transaction_value', label: 'Avg Transaction Value' },
              { key: 'service_dependency', label: 'Service Dependency (0-1)' },
              { key: 'loss_probability', label: 'Loss Probability (0-1)' },
              { key: 'operational_cost_per_hour', label: 'Operational Cost / Hour' },
              { key: 'penalty_fixed', label: 'Penalty Fixed' },
              { key: 'recovery_fixed', label: 'Recovery Fixed' },
            ].map(({ key, label }) => (
              <div className="form-group" key={key}>
                <label className="form-label">{label}</label>
                <input
                  type="number" className="form-input"
                  value={(form as any)[key]}
                  onChange={e => setF(key, e.target.value)}
                  id={`profile-${key}-input`}
                />
              </div>
            ))}
          </div>

          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn primary" onClick={save} id="save-profile-btn">💾 Save Profile</button>
            <button className="btn" onClick={() => { setEditing(null); setMsg(''); }} id="clear-profile-btn">✕ Clear</button>
          </div>
          {msg && <div className={`toast ${msgType === 'error' ? 'error' : ''}`}>{msgType === 'success' ? '✓' : '✕'} {msg}</div>}
          <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 12 }}>
            Edits bersifat versioned — tidak menimpa asumsi historis sehingga kalkulasi insiden tetap auditable.
          </p>
        </div>
      </div>
    </>
  );
}

// ─── Shared: KPI Card ─────────────────────────────────────────────────────────
function KpiCard({ label, value, icon, meta, cls = '' }: {
  label: string; value: string | number; icon: string; meta: string; cls?: string;
}) {
  return (
    <div className={`kpi-card ${cls}`} id={`kpi-${label.toLowerCase().replace(/\s+/g, '-')}`}>
      <div className="kpi-header">
        <span className="kpi-label">{label}</span>
        <span className="kpi-icon">{icon}</span>
      </div>
      <div className="kpi-value">{value}</div>
      <div className="kpi-meta">{meta}</div>
    </div>
  );
}
