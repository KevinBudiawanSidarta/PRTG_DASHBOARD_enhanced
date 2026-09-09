-- ============================================================
-- Migration 003: BIA Full Feature Enhancements
-- ============================================================

-- 1. Enrich business_services with BIA fields
ALTER TABLE business_services
  ADD COLUMN IF NOT EXISTS owner_name          TEXT,
  ADD COLUMN IF NOT EXISTS affected_users      INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS sla_target_pct      NUMERIC(5,2) NOT NULL DEFAULT 99.9,
  ADD COLUMN IF NOT EXISTS rto_minutes         INT NOT NULL DEFAULT 60,
  ADD COLUMN IF NOT EXISTS rpo_minutes         INT NOT NULL DEFAULT 30,
  ADD COLUMN IF NOT EXISTS description         TEXT,
  ADD COLUMN IF NOT EXISTS business_value_per_hour NUMERIC(18,2) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS updated_at          TIMESTAMPTZ NOT NULL DEFAULT now();

-- 2. Enrich financial_profiles with employee productivity fields
ALTER TABLE financial_profiles
  ADD COLUMN IF NOT EXISTS affected_employees          INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS avg_employee_cost_per_hour  NUMERIC(18,2) NOT NULL DEFAULT 0;

-- 3. Business Impact Matrix table (CRUD-able, maps technical condition -> business impact)
CREATE TABLE IF NOT EXISTS impact_matrix_entries (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id      UUID NOT NULL REFERENCES organizations(id),
  technical_condition  TEXT NOT NULL,
  operational_impact   TEXT NOT NULL,
  business_impact      TEXT NOT NULL,
  affected_services    TEXT[] DEFAULT '{}',
  severity             TEXT NOT NULL DEFAULT 'MAJOR'
                         CHECK (severity IN ('INFO','WARNING','MINOR','MAJOR','CRITICAL')),
  sort_order           INT NOT NULL DEFAULT 0,
  is_active            BOOLEAN NOT NULL DEFAULT true,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS impact_matrix_org_idx ON impact_matrix_entries (organization_id, sort_order);
ALTER TABLE impact_matrix_entries ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
  EXECUTE 'DROP POLICY IF EXISTS tenant_isolation_impact_matrix_entries ON impact_matrix_entries';
  EXECUTE 'CREATE POLICY tenant_isolation_impact_matrix_entries ON impact_matrix_entries '
       || 'USING (organization_id = current_setting(''app.current_org_id'', true)::uuid) '
       || 'WITH CHECK (organization_id = current_setting(''app.current_org_id'', true)::uuid)';
END $$;

-- 4. Seed default Impact Matrix entries for the default org
-- (will only insert if table is empty for org to avoid duplicates on re-run)
DO $$
DECLARE
  v_org UUID := '11111111-1111-1111-1111-111111111111';
BEGIN
  IF NOT EXISTS (SELECT 1 FROM impact_matrix_entries WHERE organization_id = v_org LIMIT 1) THEN
    INSERT INTO impact_matrix_entries (organization_id, technical_condition, operational_impact, business_impact, affected_services, severity, sort_order) VALUES
      (v_org, 'Internet Link Down',          'No external network connectivity',          'E-commerce & customer portal offline, transactions stop',            ARRAY['Customer Portal','ERP Production'],      'CRITICAL', 10),
      (v_org, 'Core Router Down',            'Complete network outage',                   'All business systems unreachable, full operational halt',            ARRAY['All Services'],                         'CRITICAL', 20),
      (v_org, 'Database Server Unavailable', 'Database connections rejected',             'ERP and all data-driven applications down, data corruption risk',    ARRAY['ERP Production','Warehouse System'],     'CRITICAL', 30),
      (v_org, 'Storage >90% Full',           'Write operations failing',                  'Data logging stops, transactions may fail silently',                 ARRAY['ERP Production','Backup System'],        'MAJOR',    40),
      (v_org, 'Primary Server High CPU',     'Application response time >5s',            'User productivity drops, SLA breach risk',                           ARRAY['ERP Production','Customer Portal'],      'MAJOR',    50),
      (v_org, 'Backup System Down',          'No automated backups running',              'Increased RPO risk, compliance exposure',                            ARRAY['Backup System'],                        'MINOR',    60),
      (v_org, 'Switch Port Flapping',        'Intermittent connectivity for affected VLAN','Periodic disconnections, user experience degraded',                  ARRAY['Office Network'],                       'MINOR',    70),
      (v_org, 'Firewall High Latency',       'Packet loss on external traffic',           'External API calls slow, payment gateway timeouts',                  ARRAY['Customer Portal','Payment Gateway'],     'MAJOR',    80),
      (v_org, 'DNS Resolution Failure',      'Hostname lookups failing',                  'Internal services cannot communicate by name, cascading failures',   ARRAY['All Services'],                         'CRITICAL', 90),
      (v_org, 'VPN Tunnel Down',             'Remote workers disconnected',               'Remote team cannot access internal systems, productivity loss',      ARRAY['Remote Work','ERP Production'],         'MAJOR',    100),
      (v_org, 'Wireless AP Offline',         'Wi-Fi unavailable in affected area',        'Mobile workers and warehouse scanners offline',                      ARRAY['Warehouse System','Office Network'],     'MINOR',    110),
      (v_org, 'UPS Battery Low',             'Power failure risk for protected devices',  'Risk of sudden shutdown and data loss',                              ARRAY['Server Room','Core Infrastructure'],    'MAJOR',    120);
  END IF;
END $$;

-- 5. Function to auto-update updated_at
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS business_services_updated_at ON business_services;
CREATE TRIGGER business_services_updated_at
  BEFORE UPDATE ON business_services
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS impact_matrix_updated_at ON impact_matrix_entries;
CREATE TRIGGER impact_matrix_updated_at
  BEFORE UPDATE ON impact_matrix_entries
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
