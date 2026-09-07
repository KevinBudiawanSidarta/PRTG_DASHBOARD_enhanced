package audit

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Record(ctx context.Context, pool *pgxpool.Pool, orgID, actor, action, entityType string, entityID *uuid.UUID, oldV, newV any) error {
	var oldJSON, newJSON []byte
	var err error
	if oldV != nil {
		oldJSON, err = json.Marshal(oldV)
		if err != nil {
			return err
		}
	}
	if newV != nil {
		newJSON, err = json.Marshal(newV)
		if err != nil {
			return err
		}
	}
	_, err = pool.Exec(ctx, `insert into audit_logs(organization_id,actor,action,entity_type,entity_id,old_value,new_value) values($1,$2,$3,$4,$5,$6,$7)`, orgID, actor, action, entityType, entityID, oldJSON, newJSON)
	return err
}
