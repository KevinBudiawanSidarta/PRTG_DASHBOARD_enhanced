package ingestion

import (
	"crypto/sha256"
	"fmt"
	"time"
)

func Fingerprint(instanceID, sensorID string, occurredAt time.Time, eventType string) string {
	s := fmt.Sprintf("%s|%s|%s|%s", instanceID, sensorID, occurredAt.UTC().Format(time.RFC3339), eventType)
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:])
}
