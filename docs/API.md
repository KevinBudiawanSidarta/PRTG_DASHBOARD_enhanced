# API MVP

Base URL: `http://localhost:8080`

Development authentication: send `X-Organization-ID: 11111111-1111-1111-1111-111111111111` when `DEV_AUTH=true`. Production uses the JWT path in `internal/auth`.

## Dashboard
- `GET /api/v1/dashboard/summary`
- `GET /api/v1/incidents?limit=25&cursor=...`
- `GET /api/v1/incidents/{id}`
- `POST /api/v1/incidents/{id}/ack`
- `POST /api/v1/incidents/{id}/close`
- `GET /api/v1/technical-events?limit=50`

## Business Service Mapping
- `GET /api/v1/services`
- `POST /api/v1/services` body `{"name":"Customer Transaction API","criticality":"P1"}`
- `PUT /api/v1/services/{id}` same body shape
- `DELETE /api/v1/services/{id}`
- `GET /api/v1/sensors`
- `GET /api/v1/services/{id}/mappings`
- `PUT /api/v1/services/{id}/mappings` body `{"mappings":[{"sensor_id":"uuid","dependency_weight":0.85}]}`

Mapping writes require role `admin` or `owner`.

## Financial Profile CRUD
- `GET /api/v1/financial-profiles`
- `POST /api/v1/financial-profiles`
- `PUT /api/v1/financial-profiles/{id}`
- `DELETE /api/v1/financial-profiles/{id}`

Example create/update body:
```json
{
  "business_service_id": "44444444-4444-4444-4444-444444444444",
  "hourly_revenue": 200000000,
  "transactions_per_hour": 500,
  "avg_transaction_value": 400000,
  "service_dependency": 0.85,
  "loss_probability": 0.30,
  "operational_cost_per_hour": 1000000,
  "penalty_fixed": 0,
  "recovery_fixed": 0
}
```

PUT does not overwrite the existing row. It closes the active interval and inserts a new version so historical calculations stay auditable. DELETE is also logical deactivation via `valid_to`.

## PRTG webhook
`POST /internal/prtg/events` is deliberately outside the user JWT middleware because PRTG is the producer. Protect it with `PRTG_WEBHOOK_SECRET`; send the secret in `X-PRTG-Webhook-Secret` or `?token=`.

PRTG Execute HTTP Action can POST `application/x-www-form-urlencoded`. Recommended payload:
```text
prtg_instance_id=22222222-2222-2222-2222-222222222222&sensorid=%sensorid&status=%status&device=%device&sensor=%sensor&datetime=%datetime&message=%message
```
The handler also accepts JSON for local integration tests. Both webhook and polling use the same `internal/ingestion.StoreEvent` path so fingerprinting, sensor upsert, append-only event storage, and worker notification are consistent.
