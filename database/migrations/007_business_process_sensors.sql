-- ============================================================
-- Migration 007: PRTG Business Process sensors
-- ============================================================
-- Mirrors PRTG "Business Process" sensors so business impact can be
-- calculated per business process. The collector keeps these tables in
-- sync with PRTG on every poll:
--   * business_process_sensors   one row per PRTG Business Process sensor
--   * business_process_channels  its channels (components), thresholds and
--                                member objects exactly as configured in PRTG
--   * business_process_intervals state timeline built from PRTG historic
--                                data (per-scan, ~1 minute resolution), so
--                                downtime here matches PRTG's own numbers
--                                instead of depending on the poll interval.

CREATE TABLE IF NOT EXISTS business_process_sensors (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id      UUID NOT NULL REFERENCES organizations(id),
  prtg_instance_id     UUID NOT NULL REFERENCES prtg_instances(id),
  prtg_sensor_id       TEXT NOT NULL,
  sensor_uuid          UUID REFERENCES prtg_sensors(id) ON DELETE SET NULL,
  name                 TEXT NOT NULL,
  device_name          TEXT,
  group_name           TEXT,
  state                TEXT NOT NULL DEFAULT 'unknown'
                         CHECK (state IN ('up','warning','down','unknown')),
  message              TEXT,
  scan_interval_sec    INT NOT NULL DEFAULT 60,
  prtg_uptime_pct      NUMERIC(7,4),
  prtg_downtime_pct    NUMERIC(7,4),
  prtg_stats_since     TIMESTAMPTZ,
  business_service_id  UUID REFERENCES business_services(id) ON DELETE SET NULL,
  -- Share of the full hourly loss that applies while the process is only
  -- degraded (PRTG Warning) rather than fully down.
  degraded_impact_pct  NUMERIC(5,2) NOT NULL DEFAULT 30
                         CHECK (degraded_impact_pct BETWEEN 0 AND 100),
  definition_status    TEXT NOT NULL DEFAULT 'ok'
                         CHECK (definition_status IN ('ok','unavailable')),
  history_synced_until TIMESTAMPTZ,
  last_synced_at       TIMESTAMPTZ,
  removed_at           TIMESTAMPTZ,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (prtg_instance_id, prtg_sensor_id)
);

CREATE TABLE IF NOT EXISTS business_process_channels (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id       UUID NOT NULL REFERENCES organizations(id),
  business_process_id   UUID NOT NULL REFERENCES business_process_sensors(id) ON DELETE CASCADE,
  prtg_channel_id       INT NOT NULL,
  name                  TEXT NOT NULL,
  state                 TEXT NOT NULL DEFAULT 'unknown'
                          CHECK (state IN ('up','warning','down','unknown')),
  warning_threshold_pct NUMERIC(5,2),
  error_threshold_pct   NUMERIC(5,2),
  -- [{objid, kind, name, parent, status, state, counts_as_up}]
  members               JSONB NOT NULL DEFAULT '[]',
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (business_process_id, prtg_channel_id)
);

-- prtg_channel_id 0 is PRTG's "Global State" channel (the process as a whole).
CREATE TABLE IF NOT EXISTS business_process_intervals (
  organization_id     UUID NOT NULL REFERENCES organizations(id),
  business_process_id UUID NOT NULL REFERENCES business_process_sensors(id) ON DELETE CASCADE,
  prtg_channel_id     INT NOT NULL,
  state               TEXT NOT NULL CHECK (state IN ('up','warning','down','unknown')),
  started_at          TIMESTAMPTZ NOT NULL,
  ended_at            TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (business_process_id, prtg_channel_id, started_at),
  CHECK (ended_at >= started_at)
);

CREATE INDEX IF NOT EXISTS business_process_sensors_org_idx ON business_process_sensors (organization_id);
CREATE INDEX IF NOT EXISTS business_process_intervals_time_idx ON business_process_intervals (business_process_id, prtg_channel_id, ended_at);

ALTER TABLE business_process_sensors ENABLE ROW LEVEL SECURITY;
ALTER TABLE business_process_channels ENABLE ROW LEVEL SECURITY;
ALTER TABLE business_process_intervals ENABLE ROW LEVEL SECURITY;

DO $$
DECLARE
  t TEXT;
BEGIN
  FOREACH t IN ARRAY ARRAY['business_process_sensors','business_process_channels','business_process_intervals'] LOOP
    EXECUTE format('DROP POLICY IF EXISTS tenant_isolation_%s ON %I', t, t);
    EXECUTE format('CREATE POLICY tenant_isolation_%s ON %I USING (organization_id = current_setting(''app.current_org_id'', true)::uuid) WITH CHECK (organization_id = current_setting(''app.current_org_id'', true)::uuid)', t, t);
  END LOOP;
END $$;

DROP TRIGGER IF EXISTS business_process_sensors_updated_at ON business_process_sensors;
CREATE TRIGGER business_process_sensors_updated_at
  BEFORE UPDATE ON business_process_sensors
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
