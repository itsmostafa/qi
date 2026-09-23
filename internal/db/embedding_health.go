package db

import (
	"context"
	"database/sql"
	"fmt"
)

// EmbeddingHealth partitions active chunks into mutually exclusive states.
// Orphaned means one side is absent or the pair's shape is wrong, which the
// next `qi index` repairs. Corrupt means a correctly shaped vector holds
// non-finite or all-zero values; qi never writes one, so the embedder does not
// look for it on every run, and only a forced reindex replaces it.
type EmbeddingHealth struct {
	Current  int
	Missing  int
	Stale    int
	Orphaned int
	Corrupt  int
}

// String is the one-line summary doctor and stats print.
func (h EmbeddingHealth) String() string {
	return fmt.Sprintf("%d current / %d missing / %d stale / %d orphaned / %d corrupt",
		h.Current, h.Missing, h.Stale, h.Orphaned, h.Corrupt)
}

func (db *DB) EmbeddingHealth(ctx context.Context, fingerprint string, dimension int, collection string) (EmbeddingHealth, error) {
	var h EmbeddingHealth
	filter := ""
	args := []any{}
	if collection != "" {
		filter = "AND d.collection = ?"
		args = append(args, collection)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT cv.chunk_id, cv.vector, em.chunk_id, em.dimension, em.fingerprint
		FROM chunks c
		JOIN documents d ON d.id = c.doc_id
		LEFT JOIN chunk_vectors cv ON cv.chunk_id = c.id
		LEFT JOIN embeddings em ON em.chunk_id = c.id
		WHERE d.active = 1 `+filter, args...)
	if err != nil {
		return h, fmt.Errorf("querying embedding health: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var vectorID, metadataID, storedDimension sql.NullInt64
		var blob []byte
		var storedFingerprint sql.NullString
		if err := rows.Scan(&vectorID, &blob, &metadataID, &storedDimension, &storedFingerprint); err != nil {
			return h, fmt.Errorf("scanning embedding health: %w", err)
		}
		switch {
		case !vectorID.Valid && !metadataID.Valid:
			h.Missing++
		case !vectorID.Valid || !metadataID.Valid:
			h.Orphaned++
		case !storedDimension.Valid || storedDimension.Int64 != int64(dimension) || len(blob) != dimension*4:
			h.Orphaned++
		case ValidateEmbeddingBlob(blob, dimension) != nil:
			h.Corrupt++
		case fingerprint != "" && storedFingerprint.Valid && storedFingerprint.String == fingerprint:
			h.Current++
		default:
			h.Stale++
		}
	}
	if err := rows.Err(); err != nil {
		return h, fmt.Errorf("reading embedding health: %w", err)
	}
	return h, nil
}
