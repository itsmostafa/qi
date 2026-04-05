# Named Collections

Every directory you index gets a named collection automatically. The name is derived from the directory path by stripping your home directory prefix and replacing `/` with `-`:

```sh
qi index ~/Projects/tools/qi
# → Saved collection "Projects-tools-qi" → /Users/alice/Projects/tools/qi
```

On subsequent runs for the same path, qi finds the existing collection and re-indexes without creating a duplicate:

```sh
qi index ~/Projects/tools/qi
# → Indexing "Projects-tools-qi" (/Users/alice/Projects/tools/qi)...
```

Use `--name` to choose a custom collection name instead of the auto-generated one:

```sh
qi index ~/notes --name notes
qi index ~/work/project/docs --name project
```

Re-index a collection by name:

```sh
qi index notes
```

Re-index everything (useful in a cron job or shell alias):

```sh
qi index
```

Collections are stored in `~/.config/qi/config.yaml` and can be edited directly:

```yaml
collections:
  - name: notes
    path: ~/notes
    extensions: [.md, .txt]
  - name: project
    path: ~/work/project/docs
```

Delete a collection and all its indexed data:

```sh
qi delete notes
```
