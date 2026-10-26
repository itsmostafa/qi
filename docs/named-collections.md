# Collections

A collection is one directory tree that qi indexes and searches under a single name. qi generates the name from the directory path, so each directory has exactly one name and its files are never indexed twice under different names.

The name is the directory's own name. When two collections would share a name, each takes on leading path segments until the names differ:

```sh
# Index directories (a path starts with /, ./, ../ or ~)
qi index ~/Projects/tools/qi     # saved as "qi"
qi index ~/work/notes            # saved as "notes" if no other collection is
qi index ~/personal/notes        # saved as "personal-notes"; the first becomes "work-notes"

# Re-index a collection by name
qi index qi
qi index work-notes
```

An argument without a leading `/`, `./`, `../` or `~` is read as a collection name, so index a relative directory as `qi index ./docs`, not `qi index docs`. `qi index` with no argument indexes the current directory. There is no `--name` flag; names come only from paths.

Collections are stored in the config file (`~/.config/qi/config.yaml` by default) and can be edited directly. `qi index <path>` and `qi hook install <path>` add an entry when the path has none. The `name` field records the generated name, but qi recomputes every name from the set of paths each time it loads the config:

```yaml
collections:
  - name: qi
    path: ~/Projects/tools/qi
  - name: work-notes
    path: ~/work/notes
  - name: personal-notes
    path: ~/personal/notes
    extensions: [.md, .txt]
```

When a name changes, because a colliding collection was added or the file holds a hand-written name, qi moves the indexed documents to the new name when it loads the config. In the example above, `~/work/notes` was indexed as `notes` until `~/personal/notes` was added; the file still says `name: notes` until the next `qi index ~/work/notes` rewrites it. Until then, `qi index notes` and `qi delete notes` still accept the old name, but search filters (`-c`) need the new one. `qi list` shows the current names.

`qi delete <collection>` removes the entry from the config file and deletes its indexed data.

See [Configuration](configuration.md#collections) for the other collection fields.
