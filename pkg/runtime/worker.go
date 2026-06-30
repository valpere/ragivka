package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/knowledge/ingestion"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
)

// StartWorker starts the River worker pool and blocks until ctx is cancelled,
// then performs a graceful shutdown with a 30-second drain window.
// NFR-7: external API calls (LLM, tools) happen inside worker jobs, outside any DB transaction.
func StartWorker(
	ctx context.Context,
	pool *pgxpool.Pool,
	sessions SessionRepository,
	messages MessageRepository,
	router aicore.ModelRouter,
	registry aicore.PromptRegistry,
	ingestWorker *ingestion.IngestDocumentWorker,
	retriever retrieval.Retriever,
) error {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewGenerateResponseWorker(messages, sessions, router, registry, retriever))
	river.AddWorker(workers, NewExpireSessionsWorker(sessions))
	river.AddWorker(workers, ingestWorker)

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
