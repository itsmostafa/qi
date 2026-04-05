# Named Collections

Use `--name` to save a directory as a named collection in config. You can then re-index it by name instead of path, and `qi index` with no arguments re-indexes all collections at once.

```sh
# Save directories as named collections
qi index ~/notes --name notes
qi index ~/work/project/docs --name project

# Re-index a collection by name
qi index notes

# Re-index everything (useful in a cron job or shell alias)
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
