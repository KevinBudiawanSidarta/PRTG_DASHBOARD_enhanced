-- Knowledge Base table for AI Business Impact Analysis
CREATE TABLE IF NOT EXISTS knowledge_base (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id UUID NOT NULL REFERENCES organizations(id),
  device_pattern TEXT NOT NULL,
  sensor_pattern TEXT,
  service_category TEXT NOT NULL DEFAULT 'network'
    CHECK (service_category IN ('network','server','application','database','storage','security')),
  description TEXT NOT NULL,
  hourly_loss_estimate NUMERIC(18,2) NOT NULL DEFAULT 0,
  affected_users_estimate INT DEFAULT 0,
  affected_processes TEXT[] DEFAULT '{}',
  sla_penalty_per_hour NUMERIC(18,2) DEFAULT 0,
  recovery_time_estimate_minutes INT DEFAULT 60,
  recovery_procedure TEXT,
  priority TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P1','P2','P3','P4')),
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS kb_org_idx ON knowledge_base (organization_id);
CREATE INDEX IF NOT EXISTS kb_device_pattern_idx ON knowledge_base (organization_id, device_pattern);
ALTER TABLE knowledge_base ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
  EXECUTE format(
    'DROP POLICY IF EXISTS tenant_isolation_knowledge_base ON knowledge_base');
  EXECUTE format(
    'CREATE POLICY tenant_isolation_knowledge_base ON knowledge_base '
    || 'USING (organization_id = current_setting(''app.current_org_id'', true)::uuid) '
    || 'WITH CHECK (organization_id = current_setting(''app.current_org_id'', true)::uuid)');
END $$;
