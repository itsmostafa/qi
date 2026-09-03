package db

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentFreshDatabaseOpens(t *testing.T) {
	for round := 0; round < 3; round++ {
		round := round
		t.Run(fmt.Sprintf("round-%d", round), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "qi.db")
			start := make(chan struct{})
			errs := make(chan error, 8)
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				i := i
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					database, err := Open(ctx, path)
					if err != nil {
						errs <- fmt.Errorf("open %d: %w", i, err)
						return
					}
					if _, err := database.ExecContext(ctx,
						`INSERT INTO collections(name, path) VALUES (?, ?)`,
						fmt.Sprintf("collection-%d", i), fmt.Sprintf("/tmp/%d", i)); err != nil {
						errs <- fmt.Errorf("write %d: %w", i, err)
					}
					if err := database.Close(); err != nil {
						errs <- fmt.Errorf("close %d: %w", i, err)
					}
				}()
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
			if t.Failed() {
				return
			}

			database, err := Open(ctx, path)
			if err != nil {
				t.Fatalf("final open: %v", err)
			}
			defer database.Close()
			assertDBCount(t, database, `SELECT COUNT(*) FROM collections`, 8)
			assertDBCount(t, database, `SELECT COUNT(*) FROM schema_version WHERE version BETWEEN 1 AND 4`, 4)
			assertDBCount(t, database, `SELECT COUNT(*) FROM schema_version WHERE version = 0`, 0)
			assertDBCount(t, database, `SELECT COUNT(*) FROM pragma_table_info('embeddings') WHERE name = 'fingerprint'`, 1)
			assertDBCount(t, database, `SELECT COUNT(*) FROM pragma_foreign_key_list('embeddings') WHERE "table" = 'chunks' AND on_delete = 'CASCADE'`, 1)
		})
	}
}
