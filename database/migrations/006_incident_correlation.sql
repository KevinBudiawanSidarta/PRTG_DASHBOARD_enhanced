-- Groups incidents that started within a short time window of each other,
-- surfacing likely shared-root-cause "incident storms" (e.g. one upstream
-- device failing takes many dependent sensors down at once) instead of
-- showing them as unrelated incidents. This is temporal correlation only —
-- the platform has no real network-topology graph to reason about true
-- root cause, so proximity in time is the signal used.
alter table incidents add column if not exists correlation_group_id uuid;
create index if not exists idx_incidents_correlation_group
  on incidents(organization_id, correlation_group_id)
  where correlation_group_id is not null;
