package config

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var nonAlphanumRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// SlugFromPath returns a collection name derived from a filesystem path. It is
// the directory's own name: a full-path slug produced names too long to type
// after -c, such as Library-Mobile-Documents-iCloud-md-obsidian-Documents-health.
// Use AssignCollectionNames when several paths must stay distinguishable.
func SlugFromPath(path string) string {
	return joinTail(collectionPathParts(path), 1)
}

// AssignCollectionNames names every path, lengthening only those that would
// otherwise collide. Two "notes" directories become "work-notes" and
// "personal-notes"; everything else keeps its short name.
func AssignCollectionNames(paths []string) []string {
	parts := make([][]string, len(paths))
	depth := make([]int, len(paths))
	names := make([]string, len(paths))
	for i, p := range paths {
		parts[i] = collectionPathParts(p)
		depth[i] = 1
	}

	// Terminates on its own: depth only ever grows, and only while it is below
	// the path's own segment count.
	for {
		for i := range paths {
			names[i] = joinTail(parts[i], depth[i])
		}
		groups := map[string][]int{}
		for i, n := range names {
			groups[n] = append(groups[n], i)
		}
		deepened := false
		for _, idxs := range groups {
			if len(idxs) < 2 {
				continue
			}
			// The same directory listed twice cannot be told apart, and
			// lengthening it only makes both names longer for nothing.
			if identicalPaths(parts, idxs) {
				continue
			}
			for _, i := range idxs {
				if depth[i] < len(parts[i]) {
					depth[i]++
					deepened = true
				}
			}
		}
		if !deepened {
			break
		}
	}

	// Paths sharing every segment (the same directory twice) stay identical on
	// purpose: validate() reports them. A generated numeric suffix would be
	// worse, since it depends on config order and could silently move one
	// collection's name onto another's indexed rows.
	return names
}

// identicalPaths reports whether every indexed path has the same segments.
func identicalPaths(parts [][]string, idxs []int) bool {
	for _, i := range idxs[1:] {
		if !slices.Equal(parts[i], parts[idxs[0]]) {
			return false
		}
	}
	return true
}

// joinTail slugs the last n segments of a split path.
func joinTail(parts []string, n int) string {
	if n > len(parts) {
		n = len(parts)
	}
	slug := strings.Join(parts[len(parts)-n:], "-")
	slug = strings.Trim(nonAlphanumRe.ReplaceAllString(slug, "-"), "-")
	if slug == "" {
		return "collection"
	}
	return slug
}

func collectionPathParts(path string) []string {
	clean := filepath.Clean(ExpandHome(path))
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, clean); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			clean = rel
		}
	}

	vol := filepath.VolumeName(clean)
	clean = strings.TrimPrefix(clean, vol)
	clean = strings.Trim(clean, string(filepath.Separator))
	if clean == "" || clean == "." {
		return nil
	}

	parts := strings.Split(clean, string(filepath.Separator))
	if len(parts) >= 3 && (parts[0] == "Users" || parts[0] == "home") {
		parts = parts[2:]
	}

	keep := parts[:0]
	for _, part := range parts {
		part = strings.Trim(nonAlphanumRe.ReplaceAllString(part, "-"), "-")
		if part != "" {
			keep = append(keep, part)
		}
	}
	return keep
}

func DefaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "qi", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "qi", "config.yaml")
}

func DefaultDBPath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "qi", "qi.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "qi", "qi.db")
}

func DefaultConfig() *Config {
	return &Config{
		DatabasePath: DefaultDBPath(),
		Search: SearchConfig{
			DefaultMode: "hybrid",
			BM25TopK:    50,
			VectorTopK:  50,
			RRFK:        60,
			ChunkSize:   512,
		},
	}
}

var DefaultConfigTemplate = `# qi configuration
# https://github.com/itsmostafa/qi

database_path: ~/.local/share/qi/qi.db

collections:
  # Names are generated from paths; ~/notes becomes notes.
  - name: notes
    path: ~/notes
    description: Personal notes and documents
    extensions: [.md, .txt]
    ignore: [.git, node_modules]

providers:
  # Uncomment to enable embeddings (llama.cpp / Ollama compatible)
  # embedding:
  #   name: ollama
  #   base_url: http://localhost:11434
  #   model: nomic-embed-text
  #   dimension: 768
  #   batch_size: 32
  #   max_input_chars: 24000  # safety net: truncate texts over ~6k tokens (set for 8k-token models)

  # Or use OpenAI embeddings — set OPENAI_API_KEY in your environment
  # embedding:
  #   name: openai
  #   model: text-embedding-3-small
  #   dimension: 1536

search:
  default_mode: hybrid   # lexical | hybrid (deep is an alias for hybrid)
  bm25_top_k: 50
  vector_top_k: 50
  rrf_k: 60
  chunk_size: 512
  # prefer_extensions: [.md, .txt]  # boost scores for these file types
  # extension_boost: 2.0            # multiplier applied to preferred extensions (default 2.0)
`
