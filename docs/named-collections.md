# Collections

Collection names are generated from their directory paths. This keeps each directory tied to one collection name and avoids duplicate indexed rows for the same files.

```sh
# Index directories
qi index ~/notes
qi index ~/work/project/docs

# Re-index a collection by generated name
qi index notes
qi index work-project-docs
```

Collections are stored in `~/.config/qi/config.yaml` and can be edited directly. The `name` field should match the generated name for the path; old custom names are normalized when qi loads the config.

```yaml
collections:
  - name: notes
    path: ~/notes
    extensions: [.md, .txt]
  - name: work-project-docs
    path: ~/work/project/docs
```
