package search

import (
	"context"
	"testing"
)

// TestBM25_MultilingualAndPunctuationQueries proves that queries in any
// script, or with no letters/digits at all, never reach FTS5 as an invalid
// empty MATCH expression.
func TestBM25_MultilingualAndPunctuationQueries(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `
		INSERT INTO content(hash, body) VALUES ('hcjk', 'body-cjk');
		INSERT INTO documents(collection, path, title, content_hash)
			VALUES ('test', 'cjk.md', 'CJK Doc', 'hcjk');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length, start_line, end_line)
			VALUES ('hcjk', 1, 0, '这是 中文 文档 内容', 'Intro', 0, 9, 1, 1);

		INSERT INTO content(hash, body) VALUES ('hcyr', 'body-cyr');
		INSERT INTO documents(collection, path, title, content_hash)
			VALUES ('test', 'cyr.md', 'Cyrillic Doc', 'hcyr');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length, start_line, end_line)
			VALUES ('hcyr', 2, 0, 'привет мир программирование', 'Intro', 0, 27, 1, 1);

		INSERT INTO content(hash, body) VALUES ('hara', 'body-ara');
		INSERT INTO documents(collection, path, title, content_hash)
			VALUES ('test', 'ara.md', 'Arabic Doc', 'hara');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length, start_line, end_line)
			VALUES ('hara', 3, 0, 'مرحبا بالعالم برمجة', 'Intro', 0, 20, 1, 1);

		INSERT INTO content(hash, body) VALUES ('hacc', 'body-acc');
		INSERT INTO documents(collection, path, title, content_hash)
			VALUES ('test', 'acc.md', 'Accented Doc', 'hacc');
		INSERT INTO chunks(content_hash, doc_id, seq, text, heading_path, ordinal, content_length, start_line, end_line)
			VALUES ('hacc', 4, 0, 'Le café et le résumé sont sur la façade naïve.', 'Intro', 0, 47, 1, 1);
	`); err != nil {
		t.Fatalf("seeding multilingual data: %v", err)
	}

	bm25 := NewBM25(database)

	scriptCases := []struct {
		name  string
		query string
		title string
	}{
		{"cjk", "中文", "CJK Doc"},
		{"cyrillic", "программирование", "Cyrillic Doc"},
		{"arabic", "برمجة", "Arabic Doc"},
		{"accented", "café", "Accented Doc"},
	}
	// Note: the seeded CJK text uses spaces between logical words. FTS5's
	// unicode61 tokenizer has no notion of CJK word segmentation, so a
	// continuous run of CJK characters with no separators becomes one
	// opaque token and substring queries against it won't match — that is
	// a known, deliberately deferred limitation (see plan/audit), not
	// something this fix attempts to solve. What this fix does guarantee is
	// that non-ASCII runes are no longer deleted by the sanitizer and no
	// longer produce an invalid empty MATCH expression.
	for _, tc := range scriptCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := bm25.Search(ctx, SearchOpts{Query: tc.query, TopK: 10})
			if err != nil {
				t.Fatalf("Search(%q) returned an error instead of results: %v", tc.query, err)
			}
			found := false
			for _, r := range results {
				if r.Title == tc.title {
					found = true
				}
			}
			if !found {
				t.Errorf("Search(%q) expected to find %q, got: %+v", tc.query, tc.title, results)
			}
		})
	}

	emptyResultCases := []struct {
		name  string
		query string
	}{
		{"punctuation_only", "???!!!"},
		{"emoji_only", "🎉🚀😀"},
		{"whitespace_only", "   "},
		{"empty", ""},
	}
	for _, tc := range emptyResultCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := bm25.Search(ctx, SearchOpts{Query: tc.query, TopK: 10})
			if err != nil {
				t.Fatalf("Search(%q) must not error on unusable input, got: %v", tc.query, err)
			}
			if len(results) != 0 {
				t.Errorf("Search(%q) expected no results, got: %+v", tc.query, results)
			}
		})
	}

	t.Run("mixed_script", func(t *testing.T) {
		results, err := bm25.Search(ctx, SearchOpts{Query: "café программирование 中文", TopK: 10})
		if err != nil {
			t.Fatalf("Search on mixed-script query errored: %v", err)
		}
		if len(results) == 0 {
			t.Error("expected mixed-script query to return at least one result across the seeded docs")
		}
	})
}
