'use client';
import { useCallback, useEffect, useMemo, useState } from 'react';

// ─── Types ─────────────────────────────────────────────────────────────────────
type ExecSummary = {
  services_normal: number; services_total: number; critical_services_impacted: number;
  estimated_users_affected: number; sla_compliance_pct: number;
  financial_exposure: number; open_critical_incidents: number;
};
type ServiceImpact = {
  service_id: string; service_name: string; status: 'normal' | 'degraded' | 'down';
  priority: string; owner: string; affected_users: number; sla_target_pct: number;
  availability_pct: number; business_impact: string; sensor_count: number;
  sensors_down: number; sensors_warning: number; downtime_minutes_month: number;
  business_value_per_hour: number;
};
type IncidentPriority = {
  incident_id: string; status: string; severity: string; started_at: string;
  duration_seconds: number; device: string; sensor: string; service_name: string;
  criticality: string; affected_users: number; financial_impact: number;
  score_criticality: number; score_user_impact: number; score_financial: number;
  score_urgency: number; priority_score: number; recovery_priority: string;
};
type SLARow = {
  service_name: string; sla_target_pct: number; availability_actual_pct: number;
  downtime_minutes_month: number; remaining_allowance_min: number; allowed_downtime_min: number;
  mttr_minutes: number; mtbf_hours: number; incident_count_month: number;
  rto_minutes: number; rpo_minutes: number; status: 'healthy' | 'at_risk' | 'breached';
};
type FinancialRow = {
  service_name: string; criticality: string; downtime_minutes: number; downtime_hours: number;
  revenue_loss: number; business_value_loss: number; productivity_loss: number;
  operational_cost_loss: number; sla_penalty: number; total_estimated_loss: number;
  affected_employees: number; avg_employee_cost_per_hour: number;
};
type ImpactMatrix = {
  id: string; technical_condition: string; operational_impact: string; business_impact: string;
  affected_services: string[]; severity: string; sort_order: number; is_active: boolean;
};
type CorrelationMember = { incident_id: string; severity: string; status: string; device: string; sensor: string; service_name: string };
type CorrelationGroup = {
  correlation_group_id: string; incident_count: number; open_incident_count: number;
  first_started_at: string; last_started_at: string; window_seconds: number;
  total_impact: number; incidents: CorrelationMember[];
};

const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
const ORG = process.env.NEXT_PUBLIC_ORGANIZATION_ID || '11111111-1111-1111-1111-111111111111';
const money = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 });
const pct = (v: number) => `${v.toFixed(2)}%`;

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
function fmtMinutes(m: number) {
  if (m < 0) return `${Math.abs(m).toFixed(0)}m over`;
  const h = Math.floor(m / 60), min = Math.round(m % 60);
  return h > 0 ? `${h}h ${min}m` : `${min}m`;
}
function fmtDuration(s: number) {
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
  return h ? `${h}h ${m}m` : `${m}m`;
}
function statusColor(s: string) {
  const map: Record<string, string> = {
    normal: 'var(--status-up)', up: 'var(--status-up)',
    degraded: 'var(--status-warning)', warning: 'var(--status-warning)',
    down: 'var(--status-down)', breached: 'var(--status-down)',
    at_risk: 'var(--status-warning)', healthy: 'var(--status-up)',
  };
  return map[s] || 'var(--text-muted)';
}
function statusLabel(s: string) {
  const map: Record<string, string> = {
    normal: 'Normal', degraded: 'Degraded', down: 'Down',
    healthy: 'Healthy', at_risk: 'At Risk', breached: 'Breached',
  };
  return map[s] || s;
}
function priorityBadge(p: string) {
  const colors: Record<string, string> = {
    P1: '#dc2626', P2: '#ea580c', P3: '#d97706', P4: '#16a34a',
  };
  return (
    <span style={{
      display: 'inline-block', padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
      background: `${colors[p] || '#6b7280'}14`, color: colors[p] || '#6b7280', letterSpacing: 0.5,
    }}>{p}</span>
  );
}
function severityBadge(s: string) {
  const colors: Record<string, string> = {
    CRITICAL: '#dc2626', MAJOR: '#ea580c', MINOR: '#d97706', WARNING: '#2563eb', INFO: '#16a34a',
  };
  return (
    <span style={{
      display: 'inline-block', padding: '2px 6px', borderRadius: 4, fontSize: 10, fontWeight: 600,
      background: `${colors[s] || '#6b7280'}14`, color: colors[s] || '#6b7280',
    }}>{s}</span>
  );
}

// ─── Main BIA Dashboard ────────────────────────────────────────────────────────
export default function BIADashboard() {
  const [activeSection, setActiveSection] = useState<'exec' | 'service' | 'matrix' | 'priority' | 'correlation' | 'financial' | 'sla'>('exec');
  const [execSummary, setExecSummary] = useState<ExecSummary | null>(null);
  const [serviceImpact, setServiceImpact] = useState<ServiceImpact[]>([]);
  const [incidentPriority, setIncidentPriority] = useState<IncidentPriority[]>([]);
  const [correlationGroups, setCorrelationGroups] = useState<CorrelationGroup[]>([]);
  const [slaData, setSlaData] = useState<SLARow[]>([]);
  const [financialData, setFinancialData] = useState<FinancialRow[]>([]);
  const [impactMatrix, setImpactMatrix] = useState<ImpactMatrix[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [lastRefresh, setLastRefresh] = useState(new Date());

  const refresh = useCallback(async () => {
    try {
      const [exec, svc, prio, corr, sla, fin, matrix] = await Promise.all([
        apiFetch<ExecSummary>('/api/v1/bia/executive-summary'),
        apiFetch<{ items: ServiceImpact[] }>('/api/v1/bia/service-impact'),
        apiFetch<{ items: IncidentPriority[] }>('/api/v1/bia/incident-priority'),
        apiFetch<{ items: CorrelationGroup[] }>('/api/v1/bia/incident-correlation'),
        apiFetch<{ items: SLARow[] }>('/api/v1/bia/sla-analysis'),
        apiFetch<{ items: FinancialRow[] }>('/api/v1/bia/financial-impact'),
        apiFetch<{ items: ImpactMatrix[] }>('/api/v1/impact-matrix'),
      ]);
      setExecSummary(exec);
      setServiceImpact(svc.items);
      setIncidentPriority(prio.items);
      setCorrelationGroups(corr.items);
      setSlaData(sla.items);
      setFinancialData(fin.items);
      setImpactMatrix(matrix.items);
      setError('');
      setLastRefresh(new Date());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'API error');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { refresh(); const t = setInterval(refresh, 30000); return () => clearInterval(t); }, [refresh]);

  const sections = [
    { id: 'exec', label: 'Executive Summary' },
    { id: 'service', label: 'Service Impact' },
    { id: 'matrix', label: 'Impact Matrix' },
    { id: 'priority', label: 'Incident Priority' },
    { id: 'correlation', label: 'Incident Correlation' },
    { id: 'financial', label: 'Financial Impact' },
    { id: 'sla', label: 'SLA & Downtime' },
  ] as const;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 0, height: '100%' }}>
      {/* Header */}
      <div style={{ padding: '20px 24px 0', borderBottom: '1px solid var(--border)' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
          <div>
            <h1 style={{ margin: 0, fontSize: 22, fontWeight: 700, color: 'var(--text-primary)' }}>
              Business Impact Analysis
            </h1>
            <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
              Refreshed: {lastRefresh.toLocaleTimeString()} · Auto-refresh every 30s
            </p>
          </div>
          <button className="btn sm" onClick={refresh} id="bia-refresh-btn" style={{ gap: 6 }}>
            Refresh
          </button>
        </div>
        {/* Section tabs */}
        <div style={{ display: 'flex', gap: 0, overflowX: 'auto' }}>
          {sections.map(s => (
            <button
              key={s.id}
              id={`bia-section-${s.id}`}
              onClick={() => setActiveSection(s.id)}
              style={{
                padding: '10px 16px', border: 'none', background: 'transparent', cursor: 'pointer',
                fontSize: 13, fontWeight: activeSection === s.id ? 700 : 500,
                color: activeSection === s.id ? 'var(--accent)' : 'var(--text-secondary)',
                borderBottom: activeSection === s.id ? '2px solid var(--accent)' : '2px solid transparent',
                whiteSpace: 'nowrap', transition: 'all 0.15s',
              }}
            >{s.label}</button>
          ))}
        </div>
      </div>

      {error && (
        <div className="alert-banner" style={{ margin: '12px 24px 0' }}>{error}</div>
      )}

      {/* Content */}
      <div style={{ flex: 1, overflow: 'auto', padding: 24 }}>
        {loading ? (
          <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
            Loading BIA data...
          </div>
        ) : (
          <>
            {activeSection === 'exec' && <ExecSummarySection data={execSummary} />}
            {activeSection === 'service' && <ServiceImpactSection data={serviceImpact} />}
            {activeSection === 'matrix' && <ImpactMatrixSection data={impactMatrix} onRefresh={refresh} />}
            {activeSection === 'priority' && <IncidentPrioritySection data={incidentPriority} />}
            {activeSection === 'correlation' && <CorrelationSection data={correlationGroups} />}
            {activeSection === 'financial' && <FinancialSection data={financialData} />}
            {activeSection === 'sla' && <SLASection data={slaData} />}
          </>
        )}
      </div>
    </div>
  );
}

// ─── Section: Executive Summary ─────────────────────────────────────────────────
function ExecSummarySection({ data }: { data: ExecSummary | null }) {
  if (!data) return <div style={{ color: 'var(--text-muted)', textAlign: 'center', padding: 40 }}>No data available</div>;

  const slaColor = data.sla_compliance_pct >= 99.9 ? 'var(--status-up)' : data.sla_compliance_pct >= 99.0 ? 'var(--status-warning)' : 'var(--status-down)';
  const slaLabel = data.sla_compliance_pct >= 99.9 ? 'Excellent' : data.sla_compliance_pct >= 99.0 ? 'At Risk' : 'Critical';

  const kpis = [
    {
      id: 'kpi-services-normal',
      label: 'Business Services Normal',
      value: `${data.services_normal} / ${data.services_total}`,
      sub: data.services_total > 0 ? `${Math.round(data.services_normal / data.services_total * 100)}% operational` : '—',
      color: data.services_normal === data.services_total ? 'var(--status-up)' : data.services_normal > 0 ? 'var(--status-warning)' : 'var(--status-down)',
    },
    {
      id: 'kpi-critical-impacted',
      label: 'Critical Services Impacted',
      value: String(data.critical_services_impacted),
      sub: data.critical_services_impacted === 0 ? 'All clear' : 'Requires immediate attention',
      color: data.critical_services_impacted === 0 ? 'var(--status-up)' : 'var(--status-down)',
    },
    {
      id: 'kpi-users-affected',
      label: 'Estimated Users Affected',
      value: data.estimated_users_affected.toLocaleString(),
      sub: data.estimated_users_affected === 0 ? 'No user impact' : 'Currently impacted',
      color: data.estimated_users_affected === 0 ? 'var(--status-up)' : data.estimated_users_affected > 100 ? 'var(--status-down)' : 'var(--status-warning)',
    },
    {
      id: 'kpi-sla-compliance',
      label: 'SLA Compliance (Bulan Ini)',
      value: pct(data.sla_compliance_pct),
      sub: slaLabel,
      color: slaColor,
    },
    {
      id: 'kpi-financial-exposure',
      label: 'Estimated Financial Exposure',
      value: money.format(data.financial_exposure),
      sub: 'Akumulasi bulan berjalan',
      color: data.financial_exposure === 0 ? 'var(--status-up)' : data.financial_exposure > 100000000 ? 'var(--status-down)' : 'var(--status-warning)',
    },
    {
      id: 'kpi-critical-incidents',
      label: 'Open Critical Incidents',
      value: String(data.open_critical_incidents),
      sub: data.open_critical_incidents === 0 ? 'No active critical incidents' : 'Escalation required',
      color: data.open_critical_incidents === 0 ? 'var(--status-up)' : 'var(--status-down)',
    },
  ];

  return (
    <div>
      <h2 style={{ margin: '0 0 20px', fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>
        Executive Summary — KPI Overview
      </h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: 16 }}>
        {kpis.map(k => (
          <div
            key={k.id}
            id={k.id}
            style={{
              background: 'var(--bg-surface)', border: '1px solid var(--border)',
              borderRadius: 'var(--radius)', padding: '20px 24px',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-secondary)', fontWeight: 500, marginBottom: 8 }}>
              {k.label}
            </div>
            <div style={{ fontSize: 26, fontWeight: 700, color: k.color, lineHeight: 1.1 }}>
              {k.value}
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
              {k.sub}
            </div>
          </div>
        ))}
      </div>

      {/* Business Health Bar */}
      <div style={{ marginTop: 28, background: 'var(--bg-surface)', borderRadius: 'var(--radius)', padding: 20, border: '1px solid var(--border)' }}>
        <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-primary)', marginBottom: 12 }}>Business Health Overview</div>
        <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
          {[
            { label: 'Total Services', val: data.services_total, color: 'var(--accent)' },
            { label: 'Fully Operational', val: data.services_normal, color: 'var(--status-up)' },
            { label: 'Impacted', val: data.critical_services_impacted, color: 'var(--status-down)' },
          ].map(item => (
            <div key={item.label} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <div style={{ width: 10, height: 10, borderRadius: '50%', background: item.color }} />
              <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{item.label}:</span>
              <span style={{ fontSize: 13, fontWeight: 700, color: item.color }}>{item.val}</span>
            </div>
          ))}
        </div>
        {data.services_total > 0 && (
          <div style={{ marginTop: 12, height: 8, background: 'var(--border)', borderRadius: 4, overflow: 'hidden' }}>
            <div style={{
              width: `${data.services_normal / data.services_total * 100}%`,
              height: '100%', background: 'var(--status-up)', borderRadius: 4, transition: 'width 0.5s',
            }} />
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Section: Business Service Impact ──────────────────────────────────────────
function ServiceImpactSection({ data }: { data: ServiceImpact[] }) {
  if (!data.length) return (
    <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
      <p>Belum ada business service. Tambahkan di tab <strong>Service Mapping</strong>.</p>
    </div>
  );

  return (
    <div>
      <h2 style={{ margin: '0 0 20px', fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>
        Business Service Impact
      </h2>
      <div style={{ overflowX: 'auto', background: 'var(--bg-surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius)' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Service', 'Status', 'Availability', 'Sensors', 'Users', 'Business Impact', 'SLA Target', 'Downtime', 'Priority'].map(h => (
                <th key={h} style={{ padding: '10px 14px', textAlign: 'left', fontWeight: 600, fontSize: 11, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5, whiteSpace: 'nowrap' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.map((row, i) => (
              <tr
                key={row.service_id}
                style={{
                  borderBottom: '1px solid var(--border)',
                  transition: 'background 0.15s',
                }}
              >
                <td style={{ padding: '12px 14px', fontWeight: 600, color: 'var(--text-primary)' }}>
                  <div>{row.service_name}</div>
                  {row.owner && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>Owner: {row.owner}</div>}
                </td>
                <td style={{ padding: '12px 14px', fontWeight: 600, color: statusColor(row.status), whiteSpace: 'nowrap' }}>
                  {statusLabel(row.status)}
                </td>
                <td style={{ padding: '12px 14px', fontWeight: 700, color: row.availability_pct < row.sla_target_pct ? 'var(--status-down)' : 'var(--status-up)', whiteSpace: 'nowrap' }}>
                  {pct(row.availability_pct)}
                </td>
                <td style={{ padding: '12px 14px', color: 'var(--text-secondary)' }}>
                  <span style={{ color: row.sensors_down > 0 ? 'var(--status-down)' : 'var(--text-secondary)' }}>
                    {row.sensors_down > 0 && `${row.sensors_down} down `}
                    {row.sensors_warning > 0 && `${row.sensors_warning} warn `}
                    {row.sensors_down === 0 && row.sensors_warning === 0 && `✓ ${row.sensor_count} ok`}
                  </span>
                </td>
                <td style={{ padding: '12px 14px', fontWeight: row.affected_users > 0 ? 700 : 400, color: row.affected_users > 0 ? 'var(--status-warning)' : 'var(--text-muted)' }}>
                  {row.affected_users.toLocaleString()}
                </td>
                <td style={{ padding: '12px 14px', color: 'var(--text-secondary)', maxWidth: 240 }}>
                  {row.business_impact}
                </td>
                <td style={{ padding: '12px 14px', color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                  {pct(row.sla_target_pct)}
                </td>
                <td style={{ padding: '12px 14px', color: row.downtime_minutes_month > 0 ? 'var(--status-warning)' : 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                  {row.downtime_minutes_month > 0 ? fmtMinutes(row.downtime_minutes_month) : '—'}
                </td>
                <td style={{ padding: '12px 14px' }}>{priorityBadge(row.priority)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ─── Section: Business Impact Matrix ──────────────────────────────────────────
function ImpactMatrixSection({ data, onRefresh }: { data: ImpactMatrix[]; onRefresh: () => void }) {
  const [showForm, setShowForm] = useState(false);
  const [editEntry, setEditEntry] = useState<ImpactMatrix | null>(null);
  const [form, setForm] = useState({ technical_condition: '', operational_impact: '', business_impact: '', affected_services: '', severity: 'MAJOR', sort_order: 0, is_active: true });
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);

  function openCreate() { setEditEntry(null); setForm({ technical_condition: '', operational_impact: '', business_impact: '', affected_services: '', severity: 'MAJOR', sort_order: 0, is_active: true }); setShowForm(true); }
  function openEdit(row: ImpactMatrix) { setEditEntry(row); setForm({ technical_condition: row.technical_condition, operational_impact: row.operational_impact, business_impact: row.business_impact, affected_services: row.affected_services.join(', '), severity: row.severity, sort_order: row.sort_order, is_active: row.is_active }); setShowForm(true); }

  async function handleSave() {
    setSaving(true);
    try {
      const body = { ...form, affected_services: form.affected_services.split(',').map(s => s.trim()).filter(Boolean), sort_order: Number(form.sort_order) };
      if (editEntry) {
        await apiFetch(`/api/v1/impact-matrix/${editEntry.id}`, { method: 'PUT', body: JSON.stringify(body) });
      } else {
        await apiFetch('/api/v1/impact-matrix', { method: 'POST', body: JSON.stringify(body) });
      }
      setShowForm(false);
      onRefresh();
    } catch (e) { alert(e instanceof Error ? e.message : 'Error'); }
    finally { setSaving(false); }
  }

  async function handleDelete(id: string) {
    if (!confirm('Hapus entry ini?')) return;
    setDeleting(id);
    try { await apiFetch(`/api/v1/impact-matrix/${id}`, { method: 'DELETE' }); onRefresh(); }
    catch (e) { alert(e instanceof Error ? e.message : 'Error'); }
    finally { setDeleting(null); }
  }

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
        <h2 style={{ margin: 0, fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>
          Business Impact Matrix
        </h2>
        <button className="btn" onClick={openCreate} id="create-matrix-btn">+ Tambah Entry</button>
      </div>

      {/* Form Modal */}
      {showForm && (
        <div className="overlay" onClick={() => setShowForm(false)}>
          <div className="drawer" onClick={e => e.stopPropagation()} style={{ maxWidth: 560 }}>
            <div className="drawer-header">
              <div>
                <div className="drawer-eyebrow">Impact Matrix</div>
                <div className="drawer-title">{editEntry ? 'Edit Entry' : 'Tambah Entry Baru'}</div>
              </div>
              <button className="btn sm" onClick={() => setShowForm(false)}>✕</button>
            </div>
            <div className="drawer-body">
              {[
                { field: 'technical_condition', label: 'Kondisi Teknis', placeholder: 'e.g. Internet Link Down' },
                { field: 'operational_impact', label: 'Dampak Operasional', placeholder: 'e.g. No external connectivity' },
                { field: 'business_impact', label: 'Dampak Bisnis *', placeholder: 'e.g. Sales transactions stop' },
                { field: 'affected_services', label: 'Service Terdampak (comma-separated)', placeholder: 'ERP Production, Customer Portal' },
              ].map(f => (
                <div key={f.field} style={{ marginBottom: 14 }}>
                  <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>{f.label}</label>
                  <input
                    className="form-input"
                    value={(form as any)[f.field]}
                    onChange={e => setForm(p => ({ ...p, [f.field]: e.target.value }))}
                    placeholder={f.placeholder}
                    style={{ width: '100%', padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 6, fontSize: 13, background: 'var(--surface-1)', color: 'var(--text-primary)' }}
                  />
                </div>
              ))}
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 14 }}>
                <div>
                  <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>Severity</label>
                  <select value={form.severity} onChange={e => setForm(p => ({ ...p, severity: e.target.value }))} style={{ width: '100%', padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 6, fontSize: 13, background: 'var(--surface-1)', color: 'var(--text-primary)' }}>
                    {['CRITICAL','MAJOR','MINOR','WARNING','INFO'].map(s => <option key={s} value={s}>{s}</option>)}
                  </select>
                </div>
                <div>
                  <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>Sort Order</label>
                  <input type="number" value={form.sort_order} onChange={e => setForm(p => ({ ...p, sort_order: Number(e.target.value) }))} style={{ width: '100%', padding: '8px 12px', border: '1px solid var(--border)', borderRadius: 6, fontSize: 13, background: 'var(--surface-1)', color: 'var(--text-primary)' }} />
                </div>
              </div>
              {editEntry && (
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 14 }}>
                  <input type="checkbox" id="is-active-check" checked={form.is_active} onChange={e => setForm(p => ({ ...p, is_active: e.target.checked }))} />
                  <label htmlFor="is-active-check" style={{ fontSize: 13, color: 'var(--text-secondary)' }}>Aktif</label>
                </div>
              )}
            </div>
            <div className="drawer-actions">
              <button className="btn primary" onClick={handleSave} disabled={saving} id="save-matrix-btn">{saving ? 'Menyimpan...' : 'Simpan'}</button>
              <button className="btn sm" onClick={() => setShowForm(false)}>Batal</button>
            </div>
          </div>
        </div>
      )}

      {data.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>Belum ada data impact matrix.</div>
      ) : (
        <div style={{ overflowX: 'auto', background: 'var(--bg-surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius)' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {['Kondisi Teknis', 'Dampak Operasional', 'Dampak Bisnis', 'Service Terdampak', 'Severity', 'Aksi'].map(h => (
                  <th key={h} style={{ padding: '10px 14px', textAlign: 'left', fontWeight: 600, fontSize: 11, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {data.map((row, i) => (
                <tr key={row.id} style={{ borderBottom: '1px solid var(--border)', opacity: row.is_active ? 1 : 0.5 }}>
                  <td style={{ padding: '12px 14px', fontWeight: 600, color: 'var(--text-primary)', maxWidth: 200 }}>{row.technical_condition}</td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-secondary)', maxWidth: 200 }}>{row.operational_impact}</td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-primary)', maxWidth: 240 }}>{row.business_impact}</td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-muted)', maxWidth: 200, fontSize: 12 }}>
                    {row.affected_services.length > 0 ? row.affected_services.slice(0,3).join(', ') + (row.affected_services.length > 3 ? '...' : '') : '—'}
                  </td>
                  <td style={{ padding: '12px 14px' }}>
                    {severityBadge(row.severity)}
                  </td>
                  <td style={{ padding: '12px 14px' }}>
                    <div style={{ display: 'flex', gap: 6 }}>
                      <button className="btn sm" onClick={() => openEdit(row)} style={{ fontSize: 11 }}>Edit</button>
                      <button className="btn sm danger" onClick={() => handleDelete(row.id)} disabled={deleting === row.id} style={{ fontSize: 11 }}>
                        {deleting === row.id ? '...' : 'Hapus'}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ─── Section: Incident Priority ────────────────────────────────────────────────
function IncidentPrioritySection({ data }: { data: IncidentPriority[] }) {
  const sorted = useMemo(() => [...data].sort((a, b) => b.priority_score - a.priority_score), [data]);

  const priorityColors: Record<string, string> = { P1: '#dc2626', P2: '#ea580c', P3: '#d97706', P4: '#16a34a' };

  if (!sorted.length) return (
    <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
      <p>Tidak ada incident aktif saat ini. Semua layanan beroperasi normal.</p>
    </div>
  );

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 20 }}>
        <h2 style={{ margin: 0, fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>
          Incident Priority & Recovery Order
        </h2>
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          Score = Criticality × Users × Financial × Urgency (1–5 tiap faktor)
        </div>
      </div>

      {/* Legend */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}>
        {[['P1','300–625','#dc2626'],['P2','150–299','#ea580c'],['P3','50–149','#d97706'],['P4','<50','#16a34a']].map(([p,r,c]) => (
          <div key={p} style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12 }}>
            <span style={{ display: 'inline-block', width: 8, height: 8, borderRadius: '50%', background: c as string }} />
            <strong style={{ color: c as string }}>{p}</strong><span style={{ color: 'var(--text-muted)' }}>: {r}</span>
          </div>
        ))}
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {sorted.map((row, i) => (
          <div
            key={row.incident_id}
            id={`priority-row-${row.incident_id}`}
            style={{
              background: 'var(--bg-surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', padding: '14px 18px',
              display: 'grid', gridTemplateColumns: 'auto 1fr auto auto auto',
              gap: 16, alignItems: 'center',
            }}
          >
            {/* Rank */}
            <div style={{ fontSize: 16, fontWeight: 600, color: 'var(--text-muted)', width: 28 }}>#{i + 1}</div>

            {/* Info */}
            <div>
              <div style={{ fontWeight: 700, color: 'var(--text-primary)', fontSize: 14 }}>
                {row.service_name}
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginTop: 2 }}>
                {row.device} · {row.sensor} · {fmtDuration(row.duration_seconds)}
              </div>
            </div>

            {/* Score breakdown */}
            <div style={{ display: 'flex', gap: 8 }}>
              {[
                { label: 'C', val: row.score_criticality, title: 'Criticality' },
                { label: 'U', val: row.score_user_impact, title: 'User Impact' },
                { label: 'F', val: row.score_financial, title: 'Financial' },
                { label: 'G', val: row.score_urgency, title: 'Urgency' },
              ].map(s => (
                <div key={s.label} title={s.title} style={{ textAlign: 'center', minWidth: 32 }}>
                  <div style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 600 }}>{s.label}</div>
                  <div style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>{s.val}</div>
                </div>
              ))}
            </div>

            {/* Total score */}
            <div style={{ textAlign: 'center', minWidth: 60 }}>
              <div style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 600 }}>SCORE</div>
              <div style={{ fontSize: 20, fontWeight: 700, color: priorityColors[row.recovery_priority] }}>{row.priority_score}</div>
            </div>

            {/* Priority */}
            <div style={{ textAlign: 'center', minWidth: 60 }}>
              <div style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 600, marginBottom: 4 }}>RECOVERY</div>
              {priorityBadge(row.recovery_priority)}
              <div style={{ marginTop: 4 }}>{severityBadge(row.severity)}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── Section: Incident Correlation (root-cause / temporal clustering) ──────────
function CorrelationSection({ data }: { data: CorrelationGroup[] }) {
  return (
    <div>
      <h2 style={{ margin: '0 0 8px', fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>
        Incident Correlation — Kemungkinan Root Cause Bersama
      </h2>
      <p style={{ margin: '0 0 20px', fontSize: 12, color: 'var(--text-muted)', maxWidth: 720 }}>
        Insiden yang mulai dalam rentang waktu berdekatan (≤3 menit) dikelompokkan di sini — biasanya tanda satu masalah infrastruktur (misal 1 switch/router mati) yang berdampak ke banyak sensor sekaligus. Ini korelasi berbasis waktu, bukan peta topologi jaringan asli — tetap verifikasi manual untuk root cause pastinya.
      </p>

      {data.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
          <p>Belum ada cluster insiden terdeteksi — semua insiden yang pernah terjadi berdiri sendiri (tidak ada yang dimulai berdekatan waktu).</p>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {data.map((g, i) => (
            <div key={g.correlation_group_id} style={{ background: 'var(--bg-surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius)', padding: 20 }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 14, flexWrap: 'wrap', gap: 8 }}>
                <div>
                  <div>
                    <div style={{ fontWeight: 700, fontSize: 14, color: 'var(--text-primary)' }}>
                      Cluster #{i + 1} — {g.incident_count} insiden{g.open_incident_count > 0 ? `, ${g.open_incident_count} masih aktif` : ''}
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                      Dimulai dalam rentang {g.window_seconds}s ({new Date(g.first_started_at).toLocaleString('id-ID')} → {new Date(g.last_started_at).toLocaleTimeString('id-ID')})
                    </div>
                  </div>
                </div>
                {g.total_impact > 0 && (
                  <div style={{ textAlign: 'right' }}>
                    <div style={{ fontSize: 10, color: 'var(--text-muted)', fontWeight: 600 }}>TOTAL DAMPAK CLUSTER</div>
                    <div style={{ fontSize: 16, fontWeight: 800, color: 'var(--red-text)' }}>{money.format(g.total_impact)}</div>
                  </div>
                )}
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {g.incidents.map(m => (
                  <div key={m.incident_id} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, padding: '7px 10px', background: 'var(--bg-raised)', borderRadius: 8, flexWrap: 'wrap' }}>
                    {severityBadge(m.severity)}
                    <strong style={{ color: 'var(--text-primary)' }}>{m.service_name}</strong>
                    <span style={{ color: 'var(--text-muted)' }}>· {m.sensor} @ {m.device}</span>
                    <span style={{
                      marginLeft: 'auto', fontSize: 10, fontWeight: 700, padding: '2px 8px', borderRadius: 4,
                      background: m.status === 'OPEN' ? 'var(--red-bg)' : m.status === 'ACKNOWLEDGED' ? 'var(--yellow-bg)' : 'var(--green-bg)',
                      color: m.status === 'OPEN' ? 'var(--red-text)' : m.status === 'ACKNOWLEDGED' ? 'var(--yellow-text)' : 'var(--green-text)',
                    }}>{m.status}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ─── Section: Financial Impact ─────────────────────────────────────────────────
function FinancialSection({ data }: { data: FinancialRow[] }) {
  const totalLoss = useMemo(() => data.reduce((s, r) => s + r.total_estimated_loss, 0), [data]);

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 20 }}>
        <div>
          <h2 style={{ margin: 0, fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>
            Financial Impact Estimation
          </h2>
          <p style={{ margin: '6px 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
            Berdasarkan incident aktif bulan berjalan. Formula: Revenue Loss + Business Value Loss + Productivity Loss + Operational Cost + SLA Penalty
          </p>
        </div>
        {totalLoss > 0 && (
          <div style={{ textAlign: 'right' }}>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, marginBottom: 2 }}>TOTAL ESTIMATED LOSS</div>
            <div style={{ fontSize: 24, fontWeight: 700, color: 'var(--red-text)' }}>{money.format(totalLoss)}</div>
          </div>
        )}
      </div>

      {data.length === 0 ? (
        <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
          <p>Tidak ada incident aktif. Tidak ada kerugian finansial yang terhitung.</p>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {data.filter(r => r.downtime_minutes > 0 || r.total_estimated_loss > 0).map(row => (
            <div
              key={row.service_name}
              style={{
                background: 'var(--bg-surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius)', padding: 20,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
                <div>
                  <span style={{ fontWeight: 700, fontSize: 15, color: 'var(--text-primary)' }}>{row.service_name}</span>
                  <span style={{ marginLeft: 10 }}>{priorityBadge(row.criticality)}</span>
                </div>
                <div style={{ textAlign: 'right' }}>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600 }}>DOWNTIME BULAN INI</div>
                  <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--status-warning)' }}>{fmtMinutes(row.downtime_minutes)}</div>
                </div>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: 12 }}>
                {[
                  { label: 'Revenue Loss', val: row.revenue_loss },
                  { label: 'Business Value Loss', val: row.business_value_loss },
                  { label: 'Productivity Loss', val: row.productivity_loss, sub: row.affected_employees > 0 ? `${row.affected_employees} karyawan` : undefined },
                  { label: 'Operational Cost', val: row.operational_cost_loss },
                  { label: 'SLA Penalty', val: row.sla_penalty },
                ].map(item => (
                  <div key={item.label} style={{ background: 'var(--bg-raised)', borderRadius: 'var(--radius-sm)', padding: '12px 14px' }}>
                    <div style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, marginBottom: 4 }}>{item.label}</div>
                    <div style={{ fontSize: 15, fontWeight: 600, color: item.val > 0 ? 'var(--text-primary)' : 'var(--text-muted)' }}>
                      {money.format(item.val)}
                    </div>
                    {item.sub && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>{item.sub}</div>}
                  </div>
                ))}
                <div style={{ border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)', padding: '12px 14px' }}>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, marginBottom: 4 }}>TOTAL ESTIMASI</div>
                  <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--red-text)', whiteSpace: 'nowrap' }}>{money.format(row.total_estimated_loss)}</div>
                </div>
              </div>
            </div>
          ))}
          {data.every(r => r.downtime_minutes === 0 && r.total_estimated_loss === 0) && (
            <div style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
              <p>Tidak ada downtime aktif. Semua layanan berjalan normal.</p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Section: SLA & Downtime ───────────────────────────────────────────────────
function SLASection({ data }: { data: SLARow[] }) {
  if (!data.length) return (
    <div style={{ textAlign: 'center', padding: 60, color: 'var(--text-muted)' }}>
      <p>Belum ada service. Tambahkan Business Service untuk melihat analisis SLA.</p>
    </div>
  );

  const slaStatusColor = (s: string) => ({ healthy: '#16a34a', at_risk: '#d97706', breached: '#dc2626' }[s] || '#6b7280');
  const slaStatusLabel = (s: string) => ({ healthy: 'Healthy', at_risk: 'At Risk', breached: 'Breached' }[s] || s);

  return (
    <div>
      <h2 style={{ margin: '0 0 20px', fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>
        SLA & Downtime Analysis — Bulan Berjalan
      </h2>
      <div style={{ overflowX: 'auto', background: 'var(--bg-surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius)' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Service', 'SLA Target', 'Aktual', 'Gap', 'Downtime', 'Sisa Allowance', 'MTTR', 'MTBF', 'RTO/RPO', 'Status'].map(h => (
                <th key={h} style={{ padding: '10px 14px', textAlign: 'left', fontWeight: 600, fontSize: 11, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5, whiteSpace: 'nowrap' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.map((row, i) => {
              const gap = row.availability_actual_pct - row.sla_target_pct;
              const isBreached = row.status === 'breached';
              const isRisk = row.status === 'at_risk';
              return (
                <tr
                  key={row.service_name}
                  style={{ borderBottom: '1px solid var(--border)' }}
                >
                  <td style={{ padding: '12px 14px', fontWeight: 700, color: 'var(--text-primary)', whiteSpace: 'nowrap' }}>{row.service_name}</td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>{pct(row.sla_target_pct)}</td>
                  <td style={{ padding: '12px 14px', fontWeight: 700, color: isBreached ? '#dc2626' : '#16a34a', whiteSpace: 'nowrap' }}>{pct(row.availability_actual_pct)}</td>
                  <td style={{ padding: '12px 14px', fontWeight: 600, color: gap >= 0 ? '#16a34a' : '#dc2626', whiteSpace: 'nowrap' }}>
                    {gap >= 0 ? `+${gap.toFixed(3)}%` : `${gap.toFixed(3)}%`}
                  </td>
                  <td style={{ padding: '12px 14px', color: row.downtime_minutes_month > 0 ? 'var(--status-warning)' : 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                    {row.downtime_minutes_month > 0 ? fmtMinutes(row.downtime_minutes_month) : '—'}
                  </td>
                  <td style={{ padding: '12px 14px', fontWeight: 600, color: row.remaining_allowance_min < 0 ? '#dc2626' : row.remaining_allowance_min < 30 ? '#d97706' : '#16a34a', whiteSpace: 'nowrap' }}>
                    {row.remaining_allowance_min < 0 ? `${Math.abs(row.remaining_allowance_min).toFixed(0)}m over` : `${row.remaining_allowance_min.toFixed(0)}m`}
                  </td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>
                    {row.mttr_minutes > 0 ? `${row.mttr_minutes.toFixed(0)}m` : '—'}
                  </td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>
                    {row.mtbf_hours > 0 ? `${row.mtbf_hours.toFixed(1)}h` : '—'}
                  </td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-muted)', fontSize: 11, whiteSpace: 'nowrap' }}>
                    RTO: {row.rto_minutes}m<br />RPO: {row.rpo_minutes}m
                  </td>
                  <td style={{ padding: '12px 14px', fontWeight: 700, color: slaStatusColor(row.status), whiteSpace: 'nowrap' }}>
                    {slaStatusLabel(row.status)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* SLA Summary Cards */}
      <div style={{ marginTop: 20, display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12 }}>
        {[
          { label: 'Healthy Services', count: data.filter(r => r.status === 'healthy').length, color: '#16a34a' },
          { label: 'At Risk Services', count: data.filter(r => r.status === 'at_risk').length, color: '#d97706' },
          { label: 'Breached SLA', count: data.filter(r => r.status === 'breached').length, color: '#dc2626' },
        ].map(card => (
          <div key={card.label} style={{ background: 'var(--bg-surface)', border: '1px solid var(--border)', borderRadius: 'var(--radius)', padding: '14px 18px', textAlign: 'center' }}>
            <div style={{ fontSize: 26, fontWeight: 700, color: card.color }}>{card.count}</div>
            <div style={{ fontSize: 12, color: 'var(--text-secondary)', fontWeight: 500 }}>{card.label}</div>
          </div>
        ))}
      </div>
    </div>
  );
}
