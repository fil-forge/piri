# repo

Storage directory configuration.

| Key | Default           | Env | Dynamic |
|-----|-------------------|-----|---------|
| `repo.data_dir` | `$HOME/.storacha/` | `PIRI_REPO_DATA_DIR` | No |
| `repo.temp_dir` | -                 | `PIRI_REPO_TEMP_DIR` | No |
| `repo.egress_journal_dir` | `<data_dir>/egress_tracker/journal` | `PIRI_REPO_EGRESS_JOURNAL_DIR` | No |

## Fields

### `data_dir`

Directory for persistent data (databases, blobs, claims).

### `temp_dir`

Directory for temporary files during processing.

### `egress_journal_dir`

Directory for the egress tracker's journal: the batches of retrieval receipts
that Piri has not yet reported to the egress tracker. The journal stays on
local disk even when blobs go to S3. Set it to keep the journal on a faster or
separately backed-up volume than `data_dir`. Also settable with
`--egress-journal-dir`.

Piri does not move an existing journal. Before changing the directory on a
running node, stop Piri and move the contents of the old directory to the new
one; receipts left behind are never reported.

## TOML

```toml
[repo]
data_dir = "/data/piri"
temp_dir = "/tmp/piri"
egress_journal_dir = "/fast/piri/egress-journal"
```

