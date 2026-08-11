package db

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
)

// InsertEmbedding stores a vector embedding for a chunk as a raw BLOB.
// Retained for tests that only need a vector row without metadata.
func (db *DB) InsertEmbedding(ctx context.Context, chunkID int64, embedding []float32) error {
	if err := validateEmbedding(embedding, len(embedding)); err != nil {
		return err
	}
	blob := serializeFloat32(embedding)
	_, err := db.ExecContext(ctx,
		`INSERT OR REPLACE INTO chunk_vectors(chunk_id, vector) VALUES (?, ?)`,
		chunkID, blob)
	if err != nil {
		return fmt.Errorf("inserting embedding: %w", err)
	}
	return nil
}

// UpsertEmbedding stores a chunk's vector and its embedding metadata
// (provider, model, dimension, fingerprint) atomically in one transaction,
// so a vector can never persist without matching metadata (or vice versa).
func (db *DB) UpsertEmbedding(ctx context.Context, chunkID int64, embedding []float32, provider, model string, dimension int, fingerprint string) error {
	if err := validateEmbedding(embedding, dimension); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning embedding transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	blob := serializeFloat32(embedding)
	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO chunk_vectors(chunk_id, vector) VALUES (?, ?)`,
		chunkID, blob); err != nil {
		return fmt.Errorf("storing vector: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO embeddings(chunk_id, provider, model, dimension, fingerprint)
		VALUES (?, ?, ?, ?, ?)
	`, chunkID, provider, model, dimension, fingerprint); err != nil {
		return fmt.Errorf("storing embedding metadata: %w", err)
	}

	return tx.Commit()
}

// ValidateEmbeddingBlob validates a little-endian float32 vector against its
// metadata dimension without allocating. It is shared by health checks and
// repair selection so both agree on what can safely participate in search.
func ValidateEmbeddingBlob(blob []byte, dimension int) error {
	if dimension <= 0 {
		return fmt.Errorf("embedding dimension must be positive, got %d", dimension)
	}
	if len(blob)%4 != 0 || len(blob)/4 != dimension {
		return fmt.Errorf("embedding blob has %d bytes, expected %d float32 values", len(blob), dimension)
	}
	var norm float64
	for i := 0; i < dimension; i++ {
		value := math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("embedding contains non-finite value at dimension %d", i)
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return fmt.Errorf("embedding has zero norm")
	}
	return nil
}

func validateEmbedding(embedding []float32, dimension int) error {
	if dimension <= 0 {
		return fmt.Errorf("embedding dimension must be positive, got %d", dimension)
	}
	if len(embedding) != dimension {
		return fmt.Errorf("embedding has dimension %d, expected %d", len(embedding), dimension)
	}
	var norm float64
	for i, value := range embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("embedding contains non-finite value at dimension %d", i)
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return fmt.Errorf("embedding has zero norm")
	}
	return nil
}

// serializeFloat32 encodes a float32 slice to little-endian bytes.
func serializeFloat32(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// deserializeFloat32 decodes little-endian bytes to a float32 slice.
func deserializeFloat32(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}
