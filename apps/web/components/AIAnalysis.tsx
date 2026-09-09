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

type Sensor = { id: string; prtg_sensor_id: string; device_name: string; sensor_name: string; last_known_state: string };

type AnalysisResult = {
  sensor: { id: string; device: string; sensor: string; state: string };
  matched_rules: {
    kb_id: string; device_pattern: string; description: string;
    hourly_loss: number; sla_penalty: number; priority: string;
    affected_processes: string[]; recovery_procedure: string;
    recovery_minutes: number; category: string;
  }[];
  total_hourly_loss: number;
  total_sla_penalty: number;
  affected_users: number;
  recovery_time_minutes: number;
  risk_level: string;
  recommendations: string[];
};

type AnalysisResponse = {
  summary: {
    total_devices_affected: number;
    total_hourly_impact: number;
    total_kb_rules: number;
    analysis_timestamp: string;
  };
  analysis: AnalysisResult[];
};

export default function AIAnalysis() {
  const [sensors, setSensors] = useState<Sensor[]>([]);
  const [selectedSensor, setSelectedSensor] = useState('');
  const [analysisData, setAnalysisData] = useState<AnalysisResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [autoAnalyzing, setAutoAnalyzing] = useState(false);
  const [error, setError] = useState('');
  const [expandedCard, setExpandedCard] = useState<string | null>(null);
  const [scenarioHours, setScenarioHours] = useState('1');

  useEffect(() => {
    apiFetch<{ items: Sensor[] }>('/api/v1/sensors').then(r => setSensors(r.items)).catch(() => {});
  }, []);

  const runAnalysis = useCallback(async (sensorId?: string) => {
    setLoading(true);
    try {
      const data = await apiFetch<AnalysisResponse>('/api/v1/ai/analyze', {
        method: 'POST',
        body: JSON.stringify({ query: 'analyze', sensor_id: sensorId || '' }),
      });
      setAnalysisData(data);
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Analisis gagal — API tidak dapat dihubungi');
    } finally {
      setLoading(false);
    }
  }, []);

  const runAutoAnalysis = useCallback(async () => {
    setAutoAnalyzing(true);
    await runAnalysis();
    setAutoAnalyzing(false);
  }, [runAnalysis]);

  useEffect(() => { runAutoAnalysis(); }, [runAutoAnalysis]);

  const downSensors = sensors.filter(s => s.last_known_state === 'down');
  const hours = Math.max(1, Number(scenarioHours) || 1);

  function riskColor(level: string) {
    switch (level) {
      case 'CRITICAL': return 'var(--red)';
      case 'HIGH': return '#f97316';
      case 'MEDIUM': return 'var(--yellow)';
      default: return 'var(--green)';
    }
  }
  function riskBg(level: string) {
    switch (level) {
      case 'CRITICAL': return 'var(--red-bg)';
      case 'HIGH': return 'rgba(249,115,22,0.12)';
      case 'MEDIUM': return 'var(--yellow-bg)';
      default: return 'var(--green-bg)';
    }
  }

  return (
    <>
      <div className="page-header">
        <div className="page-eyebrow">AI Business Impact Analysis</div>
        <h1 className="page-title">Analisis Dampak Bisnis Cerdas</h1>
        <p className="page-sub">
          Analisis otomatis dampak finansial berdasarkan knowledge base dan status sensor PRTG real-time.
        </p>
      </div>

      {error && (
        <div className="alert-banner" id="ai-error-banner">
          ⚠️ {error}
        </div>
      )}

      {/* Summary KPIs */}
      {analysisData && (
        <div className="kpi-grid" style={{ gridTemplateColumns: 'repeat(4,1fr)', marginBottom: 20 }}>
          <div className={`kpi-card ${analysisData.summary.total_devices_affected > 0 ? 'danger' : 'success'}`} id="kpi-ai-devices">
            <div className="kpi-header">
              <span className="kpi-label">Devices Terdampak</span>
              <span className="kpi-icon">🔴</span>
            </div>
            <div className="kpi-value">{analysisData.summary.total_devices_affected}</div>
            <div className="kpi-meta">Sensor sedang DOWN</div>
          </div>
          <div className={`kpi-card ${analysisData.summary.total_hourly_impact > 0 ? 'danger' : ''}`} id="kpi-ai-impact">
            <div className="kpi-header">
              <span className="kpi-label">Kerugian / Jam</span>
              <span className="kpi-icon">💸</span>
            </div>
            <div className="kpi-value" style={{ fontSize: analysisData.summary.total_hourly_impact > 999999999 ? 22 : 28 }}>
              {money.format(analysisData.summary.total_hourly_impact)}
            </div>
            <div className="kpi-meta">Estimasi per jam downtime</div>
          </div>
          <div className="kpi-card" id="kpi-ai-scenario">
            <div className="kpi-header">
              <span className="kpi-label">Proyeksi {hours}h</span>
              <span className="kpi-icon">📊</span>
            </div>
            <div className="kpi-value" style={{ fontSize: 22 }}>
              {money.format(analysisData.summary.total_hourly_impact * hours)}
            </div>
            <div className="kpi-meta">Jika downtime {hours} jam</div>
          </div>
          <div className="kpi-card" id="kpi-ai-rules">
            <div className="kpi-header">
              <span className="kpi-label">Knowledge Base</span>
              <span className="kpi-icon">📚</span>
            </div>
            <div className="kpi-value">{analysisData.summary.total_kb_rules}</div>
            <div className="kpi-meta">Rules aktif</div>
          </div>
        </div>
      )}

      {/* Controls */}
      <div className="ai-controls">
        <div className="ai-control-row">
          <div className="ai-control-group">
            <label className="form-label">Scenario Simulator</label>
            <div className="ai-scenario-input">
              <span className="ai-scenario-label">Durasi downtime:</span>
              <input
                type="number" min="1" max="720" step="1"
                className="form-input" value={scenarioHours}
                onChange={e => setScenarioHours(e.target.value)}
                style={{ width: 80 }}
                id="scenario-hours-input"
              />
              <span className="ai-scenario-label">jam</span>
            </div>
          </div>
          <div className="ai-control-group">
            <label className="form-label">Analisis Sensor Spesifik</label>
            <div style={{ display: 'flex', gap: 8 }}>
              <select className="form-select" value={selectedSensor} onChange={e => setSelectedSensor(e.target.value)} id="ai-sensor-select" style={{ minWidth: 240 }}>
                <option value="">Semua sensor DOWN</option>
                {sensors.map(s => (
                  <option key={s.id} value={s.id}>
                    {s.device_name} · {s.sensor_name} [{s.last_known_state}]
                  </option>
                ))}
              </select>
              <button
                className="btn primary"
                onClick={() => runAnalysis(selectedSensor)}
                disabled={loading}
                id="run-analysis-btn"
              >
                {loading ? '⟳ Analyzing…' : '🤖 Analyze'}
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Down Sensors Alert */}
      {downSensors.length > 0 && (
        <div className="ai-alert-strip" id="ai-down-alert">
          <div className="ai-alert-icon">⚠️</div>
          <div className="ai-alert-body">
            <strong>{downSensors.length} sensor sedang DOWN</strong>
            <span>{downSensors.map(s => s.device_name).join(', ')}</span>
          </div>
          <button className="btn sm danger" onClick={() => runAutoAnalysis()} disabled={autoAnalyzing}>
            {autoAnalyzing ? '⟳ ...' : '🔄 Re-analyze'}
          </button>
        </div>
      )}

      {/* Analysis Results */}
      {loading && !analysisData && (
        <div className="panel">
          <div className="ai-loading">
            <div className="ai-loading-dots">
              <span /><span /><span />
            </div>
            <p>AI sedang menganalisis dampak bisnis…</p>
          </div>
        </div>
      )}

      {analysisData && analysisData.analysis.length === 0 && !loading && (
        <div className="panel">
          <div className="empty-state" style={{ padding: 64 }}>
            <div className="empty-icon">✅</div>
            <p>Semua sensor UP — tidak ada dampak bisnis yang terdeteksi.</p>
            <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 8 }}>
              Pilih sensor spesifik dari dropdown untuk simulasi "what-if".
            </p>
          </div>
        </div>
      )}

      {analysisData && analysisData.analysis.length > 0 && (
        <div className="ai-results-grid">
          {analysisData.analysis.map((result, idx) => {
            const isExpanded = expandedCard === result.sensor.id;
            return (
              <div
                className={`ai-result-card ${result.risk_level.toLowerCase()}`}
                key={result.sensor.id || idx}
                id={`ai-result-${result.sensor.id}`}
              >
                {/* Card Header */}
                <div className="ai-card-header" onClick={() => setExpandedCard(isExpanded ? null : result.sensor.id)}>
                  <div className="ai-card-device">
                    <div className="ai-risk-indicator" style={{ background: riskColor(result.risk_level) }} />
                    <div>
                      <div className="ai-card-device-name">{result.sensor.device}</div>
                      <div className="ai-card-sensor-name">{result.sensor.sensor}</div>
                    </div>
                  </div>
                  <div className="ai-card-badges">
                    <span className="ai-risk-badge" style={{ background: riskBg(result.risk_level), color: riskColor(result.risk_level), borderColor: riskColor(result.risk_level) }}>
                      {result.risk_level}
                    </span>
                    <span className={`status-badge ${result.sensor.state}`}>
                      <span className="status-dot" />{result.sensor.state.toUpperCase()}
                    </span>
                  </div>
                </div>

                {/* Quick Impact Summary */}
                <div className="ai-card-impact-row">
                  <div className="ai-impact-item">
                    <span className="ai-impact-label">Kerugian/Jam</span>
                    <span className="ai-impact-value danger">{money.format(result.total_hourly_loss)}</span>
                  </div>
                  <div className="ai-impact-item">
                    <span className="ai-impact-label">Proyeksi {hours}h</span>
                    <span className="ai-impact-value">{money.format(result.total_hourly_loss * hours)}</span>
                  </div>
                  <div className="ai-impact-item">
                    <span className="ai-impact-label">SLA Penalty/Jam</span>
                    <span className="ai-impact-value">{money.format(result.total_sla_penalty)}</span>
                  </div>
                  <div className="ai-impact-item">
                    <span className="ai-impact-label">Recovery</span>
                    <span className="ai-impact-value">{result.recovery_time_minutes}m</span>
                  </div>
                </div>

                {/* Expanded Details */}
                {isExpanded && (
                  <div className="ai-card-expanded">
                    {/* Recommendations */}
                    {result.recommendations.length > 0 && (
                      <div className="ai-section">
                        <div className="ai-section-title">🤖 AI Recommendations</div>
                        <div className="ai-rec-list">
                          {result.recommendations.map((rec, i) => (
                            <div className="ai-rec-item" key={i}>
                              <span className="ai-rec-bullet">→</span>
                              <span>{rec}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Matched Rules */}
                    {result.matched_rules.length > 0 && (
                      <div className="ai-section">
                        <div className="ai-section-title">📚 Matched Knowledge Base Rules ({result.matched_rules.length})</div>
                        {result.matched_rules.map((rule, i) => (
                          <div className="ai-rule-card" key={rule.kb_id || i}>
                            <div className="ai-rule-header">
                              <span className={`pill ${rule.priority.toLowerCase() === 'p1' ? 'critical' : rule.priority.toLowerCase() === 'p2' ? 'major' : 'minor'}`}>
                                {rule.priority}
                              </span>
                              <span className="ai-rule-category">{rule.category}</span>
                            </div>
                            <p className="ai-rule-desc">{rule.description}</p>
                            <div className="ai-rule-metrics">
                              <span>💰 {money.format(rule.hourly_loss)}/jam</span>
                              <span>⚖️ SLA: {money.format(rule.sla_penalty)}/jam</span>
                              <span>⏱️ Recovery: {rule.recovery_minutes}m</span>
                            </div>
                            {rule.affected_processes && rule.affected_processes.length > 0 && (
                              <div className="ai-rule-processes">
                                <span className="ai-rule-processes-label">Proses terdampak:</span>
                                {rule.affected_processes.map((p, j) => (
                                  <span className="ai-process-tag" key={j}>{p}</span>
                                ))}
                              </div>
                            )}
                            {rule.recovery_procedure && (
                              <div className="ai-recovery-box">
                                <div className="ai-recovery-title">📋 Recovery Procedure</div>
                                <pre className="ai-recovery-text">{rule.recovery_procedure}</pre>
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    )}

                    {result.matched_rules.length === 0 && (
                      <div className="ai-section">
                        <div className="ai-no-rules">
                          <span className="ai-no-rules-icon">📝</span>
                          <p>Belum ada Knowledge Base entry untuk device ini.</p>
                          <p style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                            Tambahkan di tab Knowledge Base untuk mendapatkan analisis dampak yang akurat.
                          </p>
                        </div>
                      </div>
                    )}

                    {/* Impact Scenario Table */}
                    <div className="ai-section">
                      <div className="ai-section-title">📊 Simulasi Skenario Waktu</div>
                      <div className="table-wrap">
                        <table id={`scenario-table-${result.sensor.id}`}>
                          <thead>
                            <tr>
                              <th>Durasi</th>
                              <th>Revenue Loss</th>
                              <th>SLA Penalty</th>
                              <th>Total Kerugian</th>
                            </tr>
                          </thead>
                          <tbody>
                            {[1, 2, 4, 8, 24].map(h => (
                              <tr key={h}>
                                <td style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 12 }}>{h} jam</td>
                                <td style={{ color: 'var(--text-danger)', fontWeight: 600 }}>{money.format(result.total_hourly_loss * h)}</td>
                                <td style={{ color: 'var(--yellow)', fontWeight: 600 }}>{money.format(result.total_sla_penalty * h)}</td>
                                <td style={{ color: 'var(--text-danger)', fontWeight: 800 }}>{money.format((result.total_hourly_loss + result.total_sla_penalty) * h)}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    </div>
                  </div>
                )}

                {/* Expand Toggle */}
                <button
                  className="ai-card-toggle"
                  onClick={() => setExpandedCard(isExpanded ? null : result.sensor.id)}
                >
                  {isExpanded ? '▲ Collapse' : '▼ Show Details & Recovery'}
                </button>
              </div>
            );
          })}
        </div>
      )}

      {/* Info Panel */}
      <div className="insight-panel" style={{ marginTop: 20 }}>
        <h3>🤖 Cara Kerja AI Analysis</h3>
        <p>
          AI engine mencocokkan sensor yang DOWN dengan aturan di <strong>Knowledge Base</strong>.
          Setiap entry KB mendefinisikan device pattern, estimasi kerugian per jam, SLA penalty,
          proses bisnis yang terdampak, dan prosedur recovery. Semakin lengkap Knowledge Base,
          semakin akurat analisis dampak bisnis.
        </p>
      </div>
    </>
  );
}
