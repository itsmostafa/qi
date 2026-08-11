package indexer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/itsmostafa/qi/internal/db"
	"github.com/itsmostafa/qi/internal/providers"
)

// Embedder generates and atomically stores embeddings for chunks whose
// vector/metadata pair is missing, stale, or invalid.
type Embedder struct {
	db          *db.DB
	provider    providers.EmbeddingProvider
	providerTag string
	fingerprint string
}

func NewEmbedder(database *db.DB, provider providers.EmbeddingProvider, providerTag, fingerprint string) *Embedder {
	return &Embedder{db: database, provider: provider, providerTag: providerTag, fingerprint: fingerprint}
}

// EmbedCollection repairs every chunk that does not have a valid vector and
// matching current metadata.
func (e *Embedder) EmbedCollection(ctx context.Context, collection string) error {
	dimension := e.provider.Dimension()
	if dimension <= 0 {
		return fmt.Errorf("embedding provider dimension must be positive, got %d", dimension)
	}
	rows, err := e.db.QueryContext(ctx, `
		SELECT c.id, c.text, cv.chunk_id, cv.vector,
		       em.chunk_id, em.dimension, em.fingerprint
		FROM chunks c
		JOIN documents d ON d.id = c.doc_id
		LEFT JOIN chunk_vectors cv ON cv.chunk_id = c.id
		LEFT JOIN embeddings em ON em.chunk_id = c.id
		WHERE d.collection = ? AND d.active = 1
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
		var vectorID, metadataID, storedDimension sql.NullInt64
		var blob []byte
		var storedFingerprint sql.NullString
		if err := rows.Scan(&row.id, &row.text, &vectorID, &blob, &metadataID, &storedDimension, &storedFingerprint); err != nil {
			return fmt.Errorf("scanning embeddings to repair: %w", err)
		}
		valid := vectorID.Valid && metadataID.Valid && storedDimension.Valid &&
			int(storedDimension.Int64) == dimension && storedFingerprint.Valid &&
			storedFingerprint.String == e.fingerprint && db.ValidateEmbeddingBlob(blob, dimension) == nil
		if !valid {
			pending = append(pending, row)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading embeddings to repair: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("closing embeddings to repair: %w", err)
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
	if len(embeddings) != len(pending) {
		return fmt.Errorf("embedding provider returned %d vectors for %d texts", len(embeddings), len(pending))
	}

	// Store vector + metadata atomically per chunk, so a partial write can
	// never leave a vector without matching metadata (or the reverse).
	model := e.provider.ModelName()
	var persistErrs []error
	for i, row := range pending {
		if err := e.db.UpsertEmbedding(ctx, row.id, embeddings[i], e.providerTag, model, dimension, e.fingerprint); err != nil {
			err = fmt.Errorf("chunk %d: %w", row.id, err)
			slog.Warn("storing embedding", "chunk_id", row.id, "error", err)
			persistErrs = append(persistErrs, err)
		}
	}
	if len(persistErrs) > 0 {
		return fmt.Errorf("persisting %d of %d embeddings: %w", len(persistErrs), len(pending), errors.Join(persistErrs...))
	}

	return nil
}
