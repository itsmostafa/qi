package db

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
)

// InsertEmbedding stores a vector embedding for a chunk as a raw BLOB.
func (db *DB) InsertEmbedding(ctx context.Context, chunkID int64, embedding []float32) error {
	blob := serializeFloat32(embedding)
	_, err := db.ExecContext(ctx,
		`INSERT OR REPLACE INTO chunk_vectors(chunk_id, vector) VALUES (?, ?)`,
		chunkID, blob)
	if err != nil {
		return fmt.Errorf("inserting embedding: %w", err)
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
