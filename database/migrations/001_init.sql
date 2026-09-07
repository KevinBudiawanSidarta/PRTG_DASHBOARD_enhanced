create extension if not exists pgcrypto;

create table if not exists organizations (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  slug text unique not null,
  created_at timestamptz not null default now()
);

create table if not exists prtg_instances (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  name text,
  base_url text not null,
  api_key_encrypted bytea,
  poll_interval_sec int not null default 60 check (poll_interval_sec > 0),
  last_sync_at timestamptz,
  sync_status text not null default 'idle' check (sync_status in ('idle','syncing','error')),
  created_at timestamptz not null default now()
);

create table if not exists prtg_sensors (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  prtg_instance_id uuid not null references prtg_instances(id),
  prtg_sensor_id text not null,
  device_name text,
  sensor_name text,
  last_known_state text,
  updated_at timestamptz not null default now(),
  unique (prtg_instance_id, prtg_sensor_id)
);

create table if not exists technical_events (
  id uuid not null default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  prtg_sensor_id uuid not null references prtg_sensors(id),
  event_type text not null check (event_type in ('state_change','threshold','notification')),
  state text check (state in ('up','down','warning','unknown')),
  occurred_at timestamptz not null,
  ingested_at timestamptz not null default now(),
  fingerprint text not null,
  raw_payload jsonb not null,
  primary key (id, occurred_at)
) partition by range (occurred_at);

create table if not exists technical_events_fingerprint_uq
  (id uuid not null, occurred_at timestamptz not null, fingerprint text not null);

create unique index if not exists technical_events_fingerprint_unique
  on technical_events (fingerprint, occurred_at);

create table if not exists incidents (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  prtg_sensor_id uuid not null references prtg_sensors(id),
  status text not null default 'OPEN' check (status in ('OPEN','ACKNOWLEDGED','RESOLVED','CLOSED')),
  severity text not null check (severity in ('INFO','WARNING','MINOR','MAJOR','CRITICAL')),
  started_at timestamptz not null,
  ended_at timestamptz,
  duration_seconds int,
  created_at timestamptz not null default now()
);

create table if not exists incident_events (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  incident_id uuid not null references incidents(id),
  event_type text not null,
  actor text,
  metadata jsonb,
  created_at timestamptz not null default now()
);

create table if not exists business_services (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  name text not null,
  criticality text not null default 'P3' check (criticality in ('P1','P2','P3','P4')),
  created_at timestamptz not null default now()
);

create table if not exists service_sensor_mapping (
  organization_id uuid not null references organizations(id),
  business_service_id uuid not null references business_services(id),
  prtg_sensor_id uuid not null references prtg_sensors(id),
  dependency_weight numeric(4,3) not null default 1.0 check (dependency_weight between 0 and 1),
  primary key (business_service_id, prtg_sensor_id)
);

create table if not exists business_processes (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  business_service_id uuid not null references business_services(id),
  name text not null,
  created_at timestamptz not null default now()
);

create table if not exists financial_profiles (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  business_service_id uuid references business_services(id),
  hourly_revenue numeric(18,2) not null,
  transactions_per_hour numeric(12,2),
  avg_transaction_value numeric(18,2),
  service_dependency numeric(4,3) not null default 1.0 check (service_dependency between 0 and 1),
  loss_probability numeric(4,3) not null default 1.0 check (loss_probability between 0 and 1),
  operational_cost_per_hour numeric(18,2) not null default 0,
  penalty_config jsonb,
  recovery_config jsonb,
  valid_from timestamptz not null default now(),
  valid_to timestamptz
);

create table if not exists impact_models (
  id uuid primary key default gen_random_uuid(),
  version text not null unique,
  formula_spec jsonb not null,
  is_active boolean not null default false,
  published_at timestamptz
);

create table if not exists impact_calculations (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  incident_id uuid not null references incidents(id),
  impact_model_id uuid not null references impact_models(id),
  input_snapshot jsonb not null,
  result_breakdown jsonb not null,
  total_impact numeric(18,2) not null,
  confidence numeric(4,3),
  calculated_at timestamptz not null default now(),
  unique (incident_id, impact_model_id)
);

create table if not exists audit_logs (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id),
  actor text not null,
  action text not null,
  entity_type text not null,
  entity_id uuid,
  old_value jsonb,
  new_value jsonb,
  created_at timestamptz not null default now()
);

create index if not exists incidents_org_status_idx on incidents (organization_id, status, started_at desc);
create index if not exists incidents_org_started_idx on incidents (organization_id, started_at desc);
create index if not exists impact_calc_org_idx on impact_calculations (organization_id, calculated_at desc);
create index if not exists technical_events_sensor_time_idx on technical_events (organization_id, prtg_sensor_id, occurred_at desc);
create index if not exists audit_logs_org_time_idx on audit_logs (organization_id, created_at desc);
create unique index if not exists financial_profiles_one_active_scope_idx on financial_profiles (organization_id, coalesce(business_service_id, '00000000-0000-0000-0000-000000000000'::uuid)) where valid_to is null;

alter table organizations enable row level security;
alter table prtg_instances enable row level security;
alter table prtg_sensors enable row level security;
alter table technical_events enable row level security;
alter table incidents enable row level security;
alter table incident_events enable row level security;
alter table business_services enable row level security;
alter table service_sensor_mapping enable row level security;
alter table business_processes enable row level security;
alter table financial_profiles enable row level security;
alter table impact_calculations enable row level security;
alter table audit_logs enable row level security;

-- Tenant isolation policies. The API sets app.current_org_id for each request.
do $$
declare
  t text;
begin
  foreach t in array array[
    'organizations','prtg_instances','prtg_sensors','technical_events','incidents','incident_events',
    'business_services','service_sensor_mapping','business_processes','financial_profiles','impact_calculations','audit_logs'
  ] loop
    execute format('drop policy if exists tenant_isolation_%s on %I', t, t);
    execute format('create policy tenant_isolation_%s on %I using (organization_id = current_setting(''app.current_org_id'', true)::uuid) with check (organization_id = current_setting(''app.current_org_id'', true)::uuid)', t, t);
  end loop;
end $$;

create table if not exists technical_events_2026_09 partition of technical_events
  for values from ('2026-09-01') to ('2026-10-01');

insert into impact_models(version, formula_spec, is_active, published_at)
values ('2026.1', '{"version":"2026.1","components":[{"key":"revenue_loss","expr":"hourly_revenue * (duration_seconds/3600) * service_dependency * loss_probability"},{"key":"operational_cost","expr":"operational_cost_per_hour * (duration_seconds/3600)"},{"key":"penalty_exposure","expr":"penalty_exposure"},{"key":"recovery_cost","expr":"recovery_cost"}],"total_expr":"revenue_loss + operational_cost + penalty_exposure + recovery_cost"}', true, now())
on conflict (version) do nothing;

create or replace function notify_new_technical_event() returns trigger language plpgsql as $$
begin
  perform pg_notify('new_technical_event', NEW.id::text || '|' || NEW.occurred_at::text);
  return NEW;
end;
$$;

drop trigger if exists technical_events_notify on technical_events;
create trigger technical_events_notify after insert on technical_events for each row execute function notify_new_technical_event();
