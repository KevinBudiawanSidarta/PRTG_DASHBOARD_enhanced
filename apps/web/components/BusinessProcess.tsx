'use client';
import { useCallback, useEffect, useMemo, useState } from 'react';

// ─── Types ─────────────────────────────────────────────────────────────────────
type State = 'up' | 'warning' | 'down' | 'unknown';
type Loss = { revenue: number; productivity: number; operational: number; recovery: number; penalty: number; total: number };
type Member = { objid: string; kind: string; name: string; parent?: string; status: string; state: string; counts_as_up: boolean };
type Channel = {
  id: number; name: string; state: State;
  warning_threshold_pct: number | null; error_threshold_pct: number | null;
  members: Member[]; up_pct: number | null; down_seconds: number; degraded_seconds: number;
};
type Episode = {
  start: string; end: string; ongoing: boolean; duration_seconds: number; down_seconds: number; degraded_seconds: number;
  worst: State; causes: { channel_id: number; channel_name: string; down_seconds: number; degraded_seconds: number }[]; loss: Loss;
};
type Stats = {
  up_seconds: number; warning_seconds: number; down_seconds: number; unknown_seconds: number;
  availability_pct: number; degraded_pct: number; outages: number; degradations: number;
  mttr_seconds: number; longest_seconds: number; loss: Loss;
};
type Rates = {
  revenue_per_hour: number; productivity_per_hour: number; operational_per_hour: number; full_per_hour: number;
  degraded_factor: number; recovery_fixed: number; penalty_fixed: number; rto_seconds: number;
};
type Process = {
  id: string; prtg_sensor_id: string; name: string; device_name: string; group_name: string;
  state: State; message: string; last_synced_at: string | null; definition_status: string; removed_at: string | null;
  prtg: { uptime_pct: number; downtime_pct: number; since: string | null } | null;
  service: { id: string; name: string; criticality: string; owner_name: string; affected_users: number; sla_target_pct: number; rto_minutes: number } | null;
  financial_profile_source: 'service' | 'organization_default' | 'none';
  degraded_impact_pct: number;
  rates: Rates;
  current: { state: State; loss_per_hour: number; since?: string; duration_seconds?: number; down_seconds?: number; loss_so_far?: number; rto_remaining_seconds?: number };
  stats: Stats;
  sla: { target_pct: number; allowed_down_seconds: number; remaining_seconds: number; status: 'healthy' | 'at_risk' | 'breached' } | null;
  channels: Channel[]; episodes: Episode[]; warnings: string[];
};
type Payload = {
  period: { key: string; from: string; to: string };
  sync: { last_synced_at: string | null; poll_interval_sec: number; stale: boolean };
  summary: { total: number; down: number; degraded: number; unlinked: number; current_loss_per_hour: number; period_loss: number; avg_availability_pct?: number };
  items: Process[];
  services: { id: string; name: string; criticality: string }[];
};

const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
const ORG = process.env.NEXT_PUBLIC_ORGANIZATION_ID || '11111111-1111-1111-1111-111111111111';
const money = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 });
const PERIODS = [{ key: 'mtd', label: 'Bulan ini' }, { key: '7d', label: '7 hari' }, { key: '30d', label: '30 hari' }];
const STATE_LABEL: Record<string, string> = { up: 'Up', warning: 'Warning', down: 'Down', unknown: 'Unknown', paused: 'Paused' };

async function apiFetch<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API}${path}`, {
    ...opts,
    headers: { 'Content-Type': 'application/json', 'X-Organization-ID': ORG, ...opts.headers },
    cache: 'no-store',
  });
  if (!res.ok) throw new Error(await res.text());
  return res.status === 204 ? ({} as T) : res.json();
}

// ─── Helpers ───────────────────────────────────────────────────────────────────
function fmtDur(sec: number) {
  if (!sec || sec < 1) return '0 mnt';
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.round((sec % 3600) / 60);
  if (d) return `${d} hari ${h} jam`;
  if (h) return `${h} jam ${m} mnt`;
  return `${Math.max(m, 1)} mnt`;
}
function fmtTime(iso: string) {
  return new Date(iso).toLocaleString('id-ID', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' });
}
function ago(iso: string | null) {
  if (!iso) return 'belum pernah';
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return 'baru saja';
  if (s < 3600) return `${Math.floor(s / 60)} menit lalu`;
  if (s < 86400) return `${Math.floor(s / 3600)} jam lalu`;
  return `${Math.floor(s / 86400)} hari lalu`;
}
const pct = (v: number | null | undefined, digits = 2) => (v == null ? '—' : `${v.toFixed(digits)}%`);

function StateBadge({ state }: { state: string }) {
  const cls = state === 'paused' ? 'unknown' : state;
  return <span className={`status-badge ${cls}`}><span className="status-dot" />{STATE_LABEL[state] || state}</span>;
}

// ─── Page ──────────────────────────────────────────────────────────────────────
export default function BusinessProcess() {
  const [period, setPeriod] = useState('mtd');
  const [data, setData] = useState<Payload | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      setData(await apiFetch<Payload>(`/api/v1/business-processes?period=${period}`));
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'API tidak dapat dihubungi');
    } finally {
      setLoading(false);
    }
  }, [period]);

  useEffect(() => {
    load();
    const t = setInterval(load, 60000);
    return () => clearInterval(t);
  }, [load]);

  async function update(id: string, body: { business_service_id?: string; degraded_impact_pct?: number }) {
    try {
      await apiFetch(`/api/v1/business-processes/${id}`, { method: 'PUT', body: JSON.stringify(body) });
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Gagal menyimpan');
    }
  }

  const active = useMemo(() => (data?.items || []).filter(p => !p.removed_at), [data]);
  const allEpisodes = useMemo(() => {
    const list: (Episode & { process: string; processState: State })[] = [];
    for (const p of data?.items || []) for (const e of p.episodes) list.push({ ...e, process: p.name, processState: p.state });
    return list.sort((a, b) => new Date(b.start).getTime() - new Date(a.start).getTime()).slice(0, 30);
  }, [data]);

  const s = data?.summary;
  const periodLabel = PERIODS.find(p => p.key === period)?.label.toLowerCase() || '';

  return (
    <>
      <div className="page-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end', gap: 16, flexWrap: 'wrap' }}>
        <div>
          <div className="page-eyebrow">Business Process Impact</div>
          <h1 className="page-title">Dampak Proses Bisnis</h1>
          <p className="page-sub">
            Status, komponen, dan riwayat per menit diambil langsung dari sensor Business Process di PRTG, lalu dihitung dampak bisnisnya.
          </p>
        </div>
        <div className="bp-segmented" role="tablist" aria-label="Periode">
          {PERIODS.map(p => (
            <button key={p.key} role="tab" aria-selected={period === p.key} className={period === p.key ? 'active' : ''} onClick={() => { setPeriod(p.key); setLoading(true); }} id={`bp-period-${p.key}`}>
              {p.label}
            </button>
          ))}
        </div>
      </div>

      {error && <div className="alert-banner" id="bp-error-banner">{error}</div>}

      {data && (
        <div className={`bp-sync${data.sync.stale ? ' stale' : ''}`} id="bp-sync-status">
          <span className="status-dot" />
          {data.sync.stale
            ? <>Data PRTG belum diperbarui sejak {ago(data.sync.last_synced_at)}. Periksa apakah service collector berjalan.</>
            : <>Tersinkron dengan PRTG {ago(data.sync.last_synced_at)} · collector menyinkronkan setiap {Math.round(data.sync.poll_interval_sec / 60) || 1} menit, riwayat status per scan PRTG</>}
        </div>
      )}

      {loading && !data ? (
        <div className="kpi-grid">{[...Array(4)].map((_, i) => <div key={i} className="skeleton" style={{ height: 112 }} />)}</div>
      ) : data && active.length === 0 ? (
        <div className="panel"><div className="empty-state" style={{ padding: 64 }}>
          <p>Belum ada sensor Business Process dari PRTG.<br />Buat sensor bertipe <strong>Business Process</strong> di PRTG; collector akan mengambilnya pada sinkronisasi berikutnya.</p>
        </div></div>
      ) : data && s && (
        <>
          <div className="kpi-grid">
            <div className={`kpi-card ${s.down > 0 ? 'danger' : ''}`} id="kpi-bp-total">
              <div className="kpi-header"><span className="kpi-label">Proses bisnis</span></div>
              <div className="kpi-value">{s.total}</div>
              <div className="kpi-meta">{s.down} down · {s.degraded} degraded · {s.total - s.down - s.degraded} normal</div>
            </div>
            <div className={`kpi-card ${s.current_loss_per_hour > 0 ? 'danger' : ''}`} id="kpi-bp-rate">
              <div className="kpi-header"><span className="kpi-label">Kerugian berjalan</span></div>
              <div className="kpi-value" style={{ fontSize: 22 }}>{money.format(s.current_loss_per_hour)}<span className="bp-unit">/jam</span></div>
              <div className="kpi-meta">Dari proses yang sedang Down atau Warning</div>
            </div>
            <div className="kpi-card" id="kpi-bp-loss">
              <div className="kpi-header"><span className="kpi-label">Estimasi kerugian {periodLabel}</span></div>
              <div className="kpi-value" style={{ fontSize: 22 }}>{money.format(s.period_loss)}</div>
              <div className="kpi-meta">{s.unlinked > 0 ? `${s.unlinked} proses belum terhubung ke service` : 'Semua proses terhubung ke service'}</div>
            </div>
            <div className="kpi-card" id="kpi-bp-availability">
              <div className="kpi-header"><span className="kpi-label">Rata-rata availability</span></div>
              <div className="kpi-value">{pct(s.avg_availability_pct)}</div>
              <div className="kpi-meta">Warning dihitung tersedia, sesuai PRTG</div>
            </div>
          </div>

          {active.map(p => (
            <ProcessCard key={p.id} p={p} services={data.services} periodLabel={periodLabel} onUpdate={update} />
          ))}

          <div className="panel" id="bp-episodes-panel" style={{ marginBottom: 16 }}>
            <div className="panel-header">
              <div>
                <div className="panel-title">Riwayat gangguan</div>
                <div className="panel-sub">Periode saat proses tidak sepenuhnya Up, beserta komponen penyebab dan estimasi kerugiannya</div>
              </div>
            </div>
            <div className="table-wrap">
              <table id="bp-episodes-table">
                <thead>
                  <tr><th>Proses</th><th>Mulai</th><th>Durasi</th><th>Down</th><th>Degraded</th><th>Penyebab</th><th>Estimasi kerugian</th></tr>
                </thead>
                <tbody>
                  {allEpisodes.length === 0 ? (
                    <tr><td colSpan={7}><div className="empty-state" style={{ padding: 32 }}><p>Tidak ada gangguan pada periode ini.</p></div></td></tr>
                  ) : allEpisodes.map((e, i) => (
                    <tr key={`${e.process}-${e.start}-${i}`}>
                      <td><strong>{e.process}</strong><small>
                        {e.ongoing
                          ? <><StateBadge state={e.processState} /> · masih berlangsung</>
                          : <><StateBadge state={e.worst} /> · terparah</>}
                      </small></td>
                      <td style={{ whiteSpace: 'nowrap' }}>{fmtTime(e.start)}</td>
                      <td>{fmtDur(e.duration_seconds)}</td>
                      <td>{e.down_seconds > 0 ? fmtDur(e.down_seconds) : '—'}</td>
                      <td>{e.degraded_seconds > 0 ? fmtDur(e.degraded_seconds) : '—'}</td>
                      <td>{e.causes.length ? e.causes.map(c => c.channel_name).join(', ') : '—'}</td>
                      <td style={{ fontWeight: 600, color: e.loss.total > 0 ? 'var(--red-text)' : 'var(--text-muted)', whiteSpace: 'nowrap' }}>{money.format(e.loss.total)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <Simulator processes={active} />

          <div className="insight-panel">
            <h3>Cara perhitungan</h3>
            <p>
              Collector membaca sensor Business Process dari PRTG pada setiap sinkronisasi: status, channel (komponen) beserta objek dan ambang batasnya, serta riwayat status per scan.
              Downtime dihitung dari riwayat itu, jadi angkanya mengikuti PRTG, bukan interval polling. Availability mengikuti definisi PRTG: Warning tetap dihitung tersedia, hanya Down yang mengurangi.
              Kerugian per jam = (revenue × dependency × loss probability) + (karyawan terdampak × biaya per jam) + biaya operasional per jam, diambil dari financial profile service yang terhubung.
              Saat Warning, berlaku persentase dampak degraded. Biaya recovery dikenakan sekali per gangguan yang sempat Down, dan penalti SLA dikenakan jika downtime melewati RTO service.
            </p>
          </div>
        </>
      )}
    </>
  );
}

// ─── Process card ────────────────────────────────────────────────────────────────
function ProcessCard({ p, services, periodLabel, onUpdate }: {
  p: Process; services: Payload['services']; periodLabel: string;
  onUpdate: (id: string, body: { business_service_id?: string; degraded_impact_pct?: number }) => void;
}) {
  const [degraded, setDegraded] = useState(String(p.degraded_impact_pct));
  useEffect(() => setDegraded(String(p.degraded_impact_pct)), [p.degraded_impact_pct]);
  const st = p.stats;
  const notUp = p.state === 'down' || p.state === 'warning';
  const components = p.channels.filter(c => c.id !== 0);

  function saveDegraded() {
    const v = Number(degraded);
    if (Number.isFinite(v) && v >= 0 && v <= 100 && v !== p.degraded_impact_pct) onUpdate(p.id, { degraded_impact_pct: v });
    else setDegraded(String(p.degraded_impact_pct));
  }

  return (
    <div className={`panel bp-card ${p.state}`} id={`bp-${p.prtg_sensor_id}`} style={{ marginBottom: 16 }}>
      <div className="panel-header" style={{ flexWrap: 'wrap', gap: 12 }}>
        <div>
          <div className="panel-title" style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {p.name} <StateBadge state={p.state} />
          </div>
          <div className="panel-sub">
            PRTG #{p.prtg_sensor_id}{p.group_name && ` · ${p.group_name}`}{p.device_name && ` / ${p.device_name}`}
            {p.service && ` · ${p.service.criticality} · ${p.service.owner_name || 'tanpa owner'}`}
          </div>
        </div>
        <div className="bp-settings">
          <label className="form-label" htmlFor={`bp-svc-${p.id}`}>Business service</label>
          <select id={`bp-svc-${p.id}`} className="form-select" value={p.service?.id || ''} onChange={e => onUpdate(p.id, { business_service_id: e.target.value })}>
            <option value="">Belum terhubung</option>
            {services.map(s => <option key={s.id} value={s.id}>{s.name} · {s.criticality}</option>)}
          </select>
          <label className="form-label" htmlFor={`bp-deg-${p.id}`} title="Persentase kerugian per jam yang berlaku saat proses berstatus Warning">Dampak saat Warning</label>
          <div className="bp-pct-input">
            <input id={`bp-deg-${p.id}`} type="number" min={0} max={100} step={5} className="form-input" value={degraded}
              onChange={e => setDegraded(e.target.value)} onBlur={saveDegraded} onKeyDown={e => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
            <span>%</span>
          </div>
        </div>
      </div>

      <div className="panel-body">
        {p.warnings.length > 0 && (
          <ul className="bp-warnings">
            {p.warnings.map((w, i) => <li key={i}>{w}</li>)}
          </ul>
        )}

        {notUp && (
          <div className={`bp-current ${p.state}`}>
            <div>
              <div className="bp-stat-label">Status saat ini</div>
              <div className="bp-stat-value">{STATE_LABEL[p.state]}{p.current.since && <> sejak {fmtTime(p.current.since)}</>}</div>
              {p.current.duration_seconds != null && <div className="bp-stat-sub">Berlangsung {fmtDur(p.current.duration_seconds)}</div>}
            </div>
            <div>
              <div className="bp-stat-label">Kerugian berjalan</div>
              <div className="bp-stat-value">{money.format(p.current.loss_per_hour)}<span className="bp-unit">/jam</span></div>
              {p.current.loss_so_far != null && <div className="bp-stat-sub">Akumulasi gangguan ini {money.format(p.current.loss_so_far)}</div>}
            </div>
            {p.current.rto_remaining_seconds != null && (
              <div>
                <div className="bp-stat-label">Sisa waktu sebelum RTO</div>
                <div className="bp-stat-value" style={{ color: p.current.rto_remaining_seconds < 0 ? 'var(--red-text)' : undefined }}>
                  {p.current.rto_remaining_seconds < 0 ? `Lewat ${fmtDur(-p.current.rto_remaining_seconds)}` : fmtDur(p.current.rto_remaining_seconds)}
                </div>
                <div className="bp-stat-sub">{p.current.rto_remaining_seconds < 0 ? `Penalti SLA ${money.format(p.rates.penalty_fixed)} berlaku` : `RTO ${p.service?.rto_minutes} menit`}</div>
              </div>
            )}
          </div>
        )}

        <div className="bp-stats">
          <div>
            <div className="bp-stat-label">Availability {periodLabel}</div>
            <div className="bp-stat-value">{pct(st.availability_pct)}</div>
            <div className="bp-stat-sub">
              {p.sla ? <>Target {p.sla.target_pct}% · <span className={`bp-sla ${p.sla.status}`}>{p.sla.status === 'healthy' ? 'Aman' : p.sla.status === 'at_risk' ? 'Berisiko' : 'Terlampaui'}</span></> : 'Tanpa target SLA'}
            </div>
          </div>
          <div>
            <div className="bp-stat-label">Downtime</div>
            <div className="bp-stat-value">{fmtDur(st.down_seconds)}</div>
            <div className="bp-stat-sub">
              {p.sla ? (p.sla.remaining_seconds >= 0 ? `Sisa jatah ${fmtDur(p.sla.remaining_seconds)}` : `Lewat jatah ${fmtDur(-p.sla.remaining_seconds)}`)
                : st.unknown_seconds > 0 ? `Tanpa data PRTG ${fmtDur(st.unknown_seconds)}` : 'Data PRTG lengkap'}
            </div>
          </div>
          <div>
            <div className="bp-stat-label">Degraded (Warning)</div>
            <div className="bp-stat-value">{fmtDur(st.warning_seconds)}</div>
            <div className="bp-stat-sub">{pct(st.degraded_pct, 1)} dari waktu tercatat</div>
          </div>
          <div>
            <div className="bp-stat-label">Gangguan</div>
            <div className="bp-stat-value">{st.outages + st.degradations}</div>
            <div className="bp-stat-sub">{st.outages} down · MTTR {st.mttr_seconds ? fmtDur(st.mttr_seconds) : '—'}</div>
          </div>
          <div>
            <div className="bp-stat-label">Estimasi kerugian</div>
            <div className="bp-stat-value" style={{ color: st.loss.total > 0 ? 'var(--red-text)' : undefined }}>{money.format(st.loss.total)}</div>
            <div className="bp-stat-sub">Laju {money.format(p.rates.full_per_hour)}/jam saat Down</div>
          </div>
          <div>
            <div className="bp-stat-label">Uptime menurut PRTG</div>
            <div className="bp-stat-value">{pct(p.prtg?.uptime_pct)}</div>
            <div className="bp-stat-sub">{p.prtg?.since ? `Kumulatif sejak ${fmtTime(p.prtg.since)}` : 'Statistik PRTG belum ada'}</div>
          </div>
        </div>

        <div className="section-header" style={{ marginTop: 20, marginBottom: 8 }}>
          <span className="section-title">Komponen (channel PRTG)</span>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr><th>Channel</th><th>Status</th><th>Objek up</th><th>Ambang</th><th>Down / Degraded {periodLabel}</th><th>Objek</th></tr>
            </thead>
            <tbody>
              {components.length === 0 ? (
                <tr><td colSpan={6}><div className="empty-state" style={{ padding: 20 }}><p>Belum ada channel.</p></div></td></tr>
              ) : components.map(c => (
                <tr key={c.id}>
                  <td><strong>{c.name}</strong></td>
                  <td><StateBadge state={c.state} /></td>
                  <td>{c.up_pct == null ? '—' : `${c.members.filter(m => m.counts_as_up).length}/${c.members.length} (${Math.round(c.up_pct)}%)`}</td>
                  <td style={{ whiteSpace: 'nowrap', fontSize: 12 }}>
                    {c.warning_threshold_pct == null ? '—' : <>Warning &lt;{c.warning_threshold_pct}%<br />Down &lt;{c.error_threshold_pct}%</>}
                  </td>
                  <td style={{ whiteSpace: 'nowrap' }}>{fmtDur(c.down_seconds)} / {fmtDur(c.degraded_seconds)}</td>
                  <td>
                    <div className="bp-members">
                      {c.members.map(m => (
                        <span key={m.objid} className={`bp-member ${m.state}${m.counts_as_up ? '' : ' counts-down'}`} title={`${m.kind} #${m.objid} · ${m.status}${m.counts_as_up ? '' : ' · dihitung Down oleh PRTG'}`}>
                          <span className="status-dot" />{m.name}{m.parent && <em> @ {m.parent}</em>}
                        </span>
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {p.service && st.loss.total > 0 && (
          <div className="bp-breakdown">
            <span>Rincian kerugian {periodLabel}:</span>
            <span>Revenue {money.format(st.loss.revenue)}</span>
            <span>Produktivitas {money.format(st.loss.productivity)}</span>
            <span>Operasional {money.format(st.loss.operational)}</span>
            <span>Recovery {money.format(st.loss.recovery)}</span>
            <span>Penalti SLA {money.format(st.loss.penalty)}</span>
          </div>
        )}
      </div>
    </div>
  );
}

// ─── What-if simulator ───────────────────────────────────────────────────────────
function Simulator({ processes }: { processes: Process[] }) {
  const linked = processes.filter(p => p.service);
  const [id, setId] = useState('');
  const [hours, setHours] = useState('1');
  const [mode, setMode] = useState<'down' | 'warning'>('down');
  const p = linked.find(x => x.id === id) || linked[0];
  if (!p) return null;

  const h = Math.max(0, Number(hours) || 0);
  const r = p.rates;
  const factor = mode === 'down' ? 1 : r.degraded_factor;
  const revenue = r.revenue_per_hour * h * factor;
  const productivity = r.productivity_per_hour * h * factor;
  const operational = r.operational_per_hour * h * factor;
  const recovery = mode === 'down' && h > 0 ? r.recovery_fixed : 0;
  const penalty = mode === 'down' && r.rto_seconds > 0 && h * 3600 > r.rto_seconds ? r.penalty_fixed : 0;
  const total = revenue + productivity + operational + recovery + penalty;

  return (
    <div className="panel" id="bp-simulator" style={{ marginBottom: 16 }}>
      <div className="panel-header">
        <div>
          <div className="panel-title">Simulasi dampak</div>
          <div className="panel-sub">Perkiraan kerugian jika sebuah proses bisnis terganggu selama durasi tertentu</div>
        </div>
      </div>
      <div className="panel-body">
        <div className="bp-sim-controls">
          <div className="form-group">
            <label className="form-label" htmlFor="bp-sim-process">Proses</label>
            <select id="bp-sim-process" className="form-select" value={p.id} onChange={e => setId(e.target.value)}>
              {linked.map(x => <option key={x.id} value={x.id}>{x.name}</option>)}
            </select>
          </div>
          <div className="form-group">
            <label className="form-label" htmlFor="bp-sim-hours">Durasi (jam)</label>
            <input id="bp-sim-hours" type="number" min={0} step={0.5} className="form-input" value={hours} onChange={e => setHours(e.target.value)} />
          </div>
          <div className="form-group">
            <span className="form-label">Kondisi</span>
            <div className="bp-segmented">
              <button className={mode === 'down' ? 'active' : ''} onClick={() => setMode('down')}>Down</button>
              <button className={mode === 'warning' ? 'active' : ''} onClick={() => setMode('warning')}>Warning ({Math.round(r.degraded_factor * 100)}%)</button>
            </div>
          </div>
        </div>
        <div className="breakdown-list bp-sim-result" style={{ marginTop: 16, marginBottom: 0 }}>
          <div className="breakdown-row"><span>Revenue hilang</span><b>{money.format(revenue)}</b></div>
          <div className="breakdown-row"><span>Produktivitas karyawan</span><b>{money.format(productivity)}</b></div>
          <div className="breakdown-row"><span>Biaya operasional</span><b>{money.format(operational)}</b></div>
          <div className="breakdown-row"><span>Biaya recovery</span><b>{money.format(recovery)}</b></div>
          <div className="breakdown-row">
            <span>Penalti SLA {r.rto_seconds > 0 && `(jika lebih dari RTO ${Math.round(r.rto_seconds / 60)} menit)`}</span>
            <b>{money.format(penalty)}</b>
          </div>
          <div className="breakdown-row" style={{ fontWeight: 700 }}><span>Total</span><b style={{ color: 'var(--red-text)' }}>{money.format(total)}</b></div>
        </div>
      </div>
    </div>
  );
}
