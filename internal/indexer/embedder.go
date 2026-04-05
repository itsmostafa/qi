package indexer

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/providers"
)

// Embedder generates and stores embeddings for unembedded chunks.
type Embedder struct {
	db       *db.DB
	provider providers.EmbeddingProvider
}

func NewEmbedder(database *db.DB, provider providers.EmbeddingProvider) *Embedder {
	return &Embedder{db: database, provider: provider}
}

// EmbedCollection embeds all unembedded chunks in a collection.
func (e *Embedder) EmbedCollection(ctx context.Context, collection string) error {
	// Fetch chunks that don't have an embedding yet
	rows, err := e.db.QueryContext(ctx, `
		SELECT c.id, c.text
		FROM chunks c
		JOIN documents d ON d.id = c.doc_id
		LEFT JOIN embeddings em ON em.chunk_id = c.id
		WHERE d.collection = ? AND d.active = 1 AND em.chunk_id IS NULL
	`, collection)
	if err != nil {
		return fmt.Errorf("fetching unembedded chunks: %w", err)
	}
	defer rows.Close()

	type chunkRow struct {
		id   int64
		text string
	}

	var pending []chunkRow
	for rows.Next() {
		var row chunkRow
		if err := rows.Scan(&row.id, &row.text); err != nil {
			return err
		}
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(pending) == 0 {
		return nil
	}

	slog.Info("embedding chunks", "count", len(pending), "collection", collection)

	// Extract texts for batch embedding
	texts := make([]string, len(pending))
	for i, row := range pending {
		texts[i] = row.text
	}

	embeddings, err := e.provider.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("generating embeddings: %w", err)
	}

	// Store embeddings
	model := e.provider.ModelName()
	for i, row := range pending {
		if err := e.db.InsertEmbedding(ctx, row.id, embeddings[i]); err != nil {
			slog.Warn("storing embedding", "chunk_id", row.id, "error", err)
			continue
		}
		_, err := e.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO embeddings(chunk_id, provider, model, dimension)
			VALUES (?, 'http', ?, ?)
		`, row.id, model, e.provider.Dimension())
		if err != nil {
			slog.Warn("recording embedding metadata", "chunk_id", row.id, "error", err)
		}
	}

	return nil
}
