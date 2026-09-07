package analytics

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Summary struct {
	OpenIncidents     int     `json:"open_incidents"`
	CriticalIncidents int     `json:"critical_incidents"`
	TotalImpact       float64 `json:"total_impact"`
	Events24h         int     `json:"events_24h"`
}

func SummaryFor(ctx context.Context, p *pgxpool.Pool, org string) (Summary, error) {
	var s Summary
	if err := p.QueryRow(ctx, `select count(*) filter(where status in ('OPEN','ACKNOWLEDGED')), count(*) filter(where severity='CRITICAL' and status<>'CLOSED') from incidents where organization_id=$1`, org).Scan(&s.OpenIncidents, &s.CriticalIncidents); err != nil {
		return s, err
	}
	if err := p.QueryRow(ctx, `select coalesce(sum(total_impact),0) from impact_calculations where organization_id=$1 and calculated_at>=now()-interval '30 days'`, org).Scan(&s.TotalImpact); err != nil {
		return s, err
	}
	if err := p.QueryRow(ctx, `select count(*) from technical_events where organization_id=$1 and ingested_at>=now()-interval '24 hours'`, org).Scan(&s.Events24h); err != nil {
		return s, err
	}
	return s, nil
}
