package financial

import (
	"github.com/shopspring/decimal"
	"testing"
)

func TestRevenueLossGolden(t *testing.T) {
	m := Model{Version: "2026.1", Components: []Component{{Key: "revenue_loss", Expr: "hourly_revenue * (duration_seconds/3600) * service_dependency * loss_probability"}}, TotalExpr: "revenue_loss"}
	in := InputSnapshot{HourlyRevenue: decimal.NewFromInt(200000000), DurationSeconds: 5400, ServiceDependency: decimal.NewFromFloat(0.65), LossProbability: decimal.NewFromFloat(0.30)}
	got, err := Calculate(m, in)
	if err != nil {
		t.Fatal(err)
	}
	want := decimal.NewFromInt(58500000)
	if !got.Breakdown["revenue_loss"].Equal(want) {
		t.Fatalf("got %s want %s", got.Breakdown["revenue_loss"], want)
	}
}
