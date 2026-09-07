package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/example/bia-platform/internal/platform/db"
)

func main() {
	ctx := context.Background()
	pool, err := db.NewPool(ctx)
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	months := 2
	if v, _ := strconv.Atoi(os.Getenv("PARTITION_MONTHS_AHEAD")); v > 0 {
		months = v
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	run := func() {
		for i := 0; i <= months; i++ {
			d := time.Now().UTC().AddDate(0, i, 0)
			first := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
			next := first.AddDate(0, 1, 0)
			name := fmt.Sprintf("technical_events_%04d_%02d", first.Year(), int(first.Month()))
			sql := fmt.Sprintf("create table if not exists %s partition of technical_events for values from ('%s') to ('%s')", name, first.Format("2006-01-02"), next.Format("2006-01-02"))
			if _, err := pool.Exec(ctx, sql); err != nil {
				log.Error("partition creation failed", "component", "scheduler", "partition", name, "error", err)
			}
		}
	}
	run()
	for range ticker.C {
		run()
	}
}
