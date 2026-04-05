package app

import (
	"context"
	"fmt"

	"github.com/itsmostafa/qi/internal/config"
	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/indexer"
	"github.com/itsmostafa/qi/internal/providers"
	"github.com/itsmostafa/qi/internal/search"
)

// App wires config, db, and services together.
type App struct {
	Config    *config.Config
	DB        *db.DB
	Indexer   *indexer.Indexer
	Embedder  *indexer.Embedder  // nil if no embedding provider configured
	BM25      *search.BM25
	Vector    *search.VectorSearch
	Hybrid    *search.Hybrid
	Asker     *search.Asker
	Generator providers.GenerationProvider // nil if not configured
}

// New opens the database and wires all services.
func New(ctx context.Context, cfgPath string) (*App, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	database, err := db.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	a := &App{
		Config:  cfg,
		DB:      database,
		Indexer: indexer.New(database, cfg.Search.ChunkSize),
		BM25:    search.NewBM25(database),
		Vector:  search.NewVectorSearch(database),
	}

	if cfg.Providers.Embedding != nil {
		embProvider := providers.NewEmbedding(cfg.Providers.Embedding)
		a.Embedder = indexer.NewEmbedder(database, embProvider)
		a.Hybrid = search.NewHybrid(a.BM25, a.Vector, embProvider, cfg.Search)
	}

	if cfg.Providers.Generation != nil {
		a.Generator = providers.NewGeneration(cfg.Providers.Generation)
		topK := cfg.Search.RerankTopK
		if topK <= 0 {
			topK = 10
		}
		a.Asker = search.NewAsker(a.Hybrid, a.BM25, a.Generator, database, topK)
	}

	return a, nil
}

// Close releases all resources.
func (a *App) Close() error {
	return a.DB.Close()
}
