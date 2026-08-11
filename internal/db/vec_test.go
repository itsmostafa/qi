package db

import (
	"context"
	"math"
	"testing"
)

func TestInsertEmbeddingRejectsInvalidVectors(t *testing.T) {
	database := openMemoryDB(t)
	defer database.Close()
	for name, vector := range map[string][]float32{
		"empty":      {},
		"zero norm":  {0, 0},
		"non-finite": {1, math.Float32frombits(0x7fc00000)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := database.InsertEmbedding(context.Background(), 1, vector); err == nil {
				t.Fatal("expected invalid vector rejection")
			}
		})
	}
}
