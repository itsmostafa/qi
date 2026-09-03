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
	Embedder  *indexer.Embedder // nil if no embedding provider configured
	BM25      *search.BM25
	Vector    *search.VectorSearch
	Hybrid    *search.Hybrid
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
	for _, col := range normalizableLegacyCollections(cfg.Collections) {
		if err := database.RenameCollectionData(ctx, col.OriginalName, col.Name, col.Path); err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("normalizing collection %q: %w", col.Name, err)
		}
	}

	// The embedding fingerprint identifies which provider endpoint and
	// model/dimension/truncation config produced a stored vector. Computed once here so the embedder
	// (writer) and vector search (reader) always agree on the active
	// identity. Empty when no embedding provider is configured.
	fingerprint := cfg.Providers.Embedding.Fingerprint()

	a := &App{
		Config:  cfg,
		DB:      database,
		Indexer: indexer.New(database, cfg.Search.ChunkSize),
		BM25:    search.NewBM25(database),
		Vector:  search.NewVectorSearch(database, fingerprint),
	}

	if cfg.Providers.Embedding != nil {
		embProvider := providers.NewEmbedding(cfg.Providers.Embedding)
		providerTag := cfg.Providers.Embedding.Name
		if providerTag == "" {
			providerTag = "http"
		}
		a.Embedder = indexer.NewEmbedder(database, embProvider, providerTag, fingerprint)
		a.Hybrid = search.NewHybrid(a.BM25, a.Vector, embProvider, cfg.Search)
	}

	return a, nil
}

// Close releases all resources.
func (a *App) Close() error {
	return a.DB.Close()
}

func normalizableLegacyCollections(collections []config.Collection) []config.Collection {
	currentNames := map[string]bool{}
	legacyNameCounts := map[string]int{}
	for _, col := range collections {
		currentNames[col.Name] = true
		if col.OriginalName != "" && col.OriginalName != col.Name {
			legacyNameCounts[col.OriginalName]++
		}
	}

	normalizable := make([]config.Collection, 0, len(collections))
	for _, col := range collections {
		if col.OriginalName == "" || col.OriginalName == col.Name {
			continue
		}
		if legacyNameCounts[col.OriginalName] > 1 {
			continue
		}
		if currentNames[col.OriginalName] {
			continue
		}
		normalizable = append(normalizable, col)
	}
	return normalizable
}
