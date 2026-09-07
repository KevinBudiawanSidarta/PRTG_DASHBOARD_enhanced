# ADR 0001 — MVP service boundaries

The repository follows the architecture specification: API, collector, worker and scheduler are separate binaries in one Go module. PostgreSQL LISTEN/NOTIFY is used for MVP event triggering. Kafka/NATS/Redis are intentionally not required for the first scale tier.
