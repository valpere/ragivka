package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// StartWorker starts the River worker pool and blocks until ctx is cancelled,
// then performs a graceful shutdown with a 30-second drain window.
// NFR-7: external API calls (LLM, tools) happen inside worker jobs, outside any DB transaction.
func StartWorker(ctx context.Context, pool *pgxpool.Pool, sessions SessionRepository) error {
	workers := river.NewWorkers()
	river.AddWorker(workers, &GenerateResponseWorker{})
	river.AddWorker(workers, NewExpireSessionsWorker(sessions))

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Workers:         workers,
		SoftStopTimeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}

	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}

	<-client.Stopped()
	return nil
}
