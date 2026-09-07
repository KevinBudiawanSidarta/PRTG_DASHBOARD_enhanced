# IT Business Impact & Financial Loss Intelligence Platform

MVP reference implementation based on `TAD-IT-Business-Impact-Platform.md`. The core architecture follows the source specification: Go multi-binary monorepo, PostgreSQL, append-only telemetry, PostgreSQL LISTEN/NOTIFY in MVP, incident state machine, versioned financial impact model, RLS tenant isolation, audit logging, and a Next.js dashboard.

## Components

- `services/api`: REST API (`/api/v1`)
- `services/collector`: PRTG polling -> append-only `technical_events`
- `services/worker`: PostgreSQL `LISTEN/NOTIFY` consumer, incident engine, financial engine
- `services/scheduler`: future-month `technical_events` partitions
- `apps/web`: Next.js executive / IT operations / financial-impact dashboard
- `database/migrations`: forward-only PostgreSQL migration
- `database/seeds`: demo tenant + service + sensor + financial profile

## Run locally

```bash
cp .env.example .env
# adjust DATABASE_URL and PRTG settings
# For the supplied PRTG server, use:
# PRTG_SERVER=http://192.168.10.22
# PRTG_USER=prtgadmin
# PRTG_PASS=<your PRTG password>

docker compose up -d postgres
psql "$DATABASE_URL" -f database/migrations/001_init.sql
psql "$DATABASE_URL" -f database/seeds/001_demo.sql
```

Run each process in a separate terminal:

```bash
go run ./services/api/cmd
go run ./services/collector/cmd
go run ./services/worker/cmd
go run ./services/scheduler/cmd
```

Run the web app:

```bash
cd apps/web
npm install
NEXT_PUBLIC_API_URL=http://localhost:8080 npm run dev
```

The demo tenant id is `11111111-1111-1111-1111-111111111111`.

## Important implementation assumptions

The architecture document defines service boundaries and business rules, but it does not fully specify PRTG response payloads or Supabase claim mapping. This MVP therefore isolates those choices:

- PRTG adapter uses the legacy sensor table endpoint `/api/table.json?content=sensors&output=json&columns=objid,device,sensor,status,lastcheck` and `username` + `passhash` credentials.
- Production JWT verification is represented by `SUPABASE_JWT_SECRET` + an `org_id` claim. Swap only `internal/auth` if your Supabase setup uses another claim/JWKS flow.
- The first collector cut detects `state_change` events from polling. Webhook ingestion can be added behind the same normalized ingestion function without changing the database contract.
- The financial expression evaluator intentionally supports only a sandboxed arithmetic subset (`+ - * /`, parentheses, and whitelisted variables). This avoids generic `eval` and keeps calculations deterministic.

## Validation note

The source environment used to assemble this repository does not have outbound package-network access, so dependency download and a full `go test` / Next.js build could not be completed here. The source code is formatted with `gofmt`; CI is included to perform dependency resolution, race-enabled tests, web build, and Trivy scanning in GitHub Actions.

## New API contracts

### Business services / mapping
```text
GET    /api/v1/services
POST   /api/v1/services
PUT    /api/v1/services/{id}
DELETE /api/v1/services/{id}
GET    /api/v1/services/{id}/mappings
PUT    /api/v1/services/{id}/mappings
GET    /api/v1/sensors
```
`PUT /api/v1/services/{id}/mappings` replaces the complete mapping for that service. Each mapping contains `sensor_id` and `dependency_weight` in the range `(0,1]`.

### Financial profiles
```text
GET    /api/v1/financial-profiles
POST   /api/v1/financial-profiles
PUT    /api/v1/financial-profiles/{id}
DELETE /api/v1/financial-profiles/{id}
```
Updates create a new profile version and close the previous active interval. DELETE is a logical deactivation (`valid_to`), preserving historical assumptions.

### PRTG webhook
```text
POST /internal/prtg/events
```
PRTG can call this endpoint using Execute HTTP Action. The implementation accepts `application/x-www-form-urlencoded` fields such as `prtg_instance_id`, `sensorid`, `status`, `device`, `sensor`, `datetime`, and optionally `organization_id`. Set `PRTG_WEBHOOK_SECRET` and either send `X-PRTG-Webhook-Secret` or add `?token=<secret>` to the URL. The endpoint also accepts JSON for testing.

Example PRTG POST payload:
```text
prtg_instance_id=22222222-2222-2222-2222-222222222222&sensorid=%sensorid&status=%status&device=%device&sensor=%sensor&datetime=%datetime&message=%message
```

When configuring PRTG, the official notification placeholders include `%sensorid`, `%status`, `%device`, `%sensor`, and `%datetime`; Execute HTTP Action supports POST with form payloads. See the Paessler docs for the exact notification-template behavior.
