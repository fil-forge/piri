# repo

Storage directory configuration.

| Key | Default           | Env | Dynamic |
|-----|-------------------|-----|---------|
| `repo.data_dir` | `$HOME/.storacha/` | `PIRI_REPO_DATA_DIR` | No |
| `repo.temp_dir` | -                 | `PIRI_REPO_TEMP_DIR` | No |
| `repo.blob_dir` | `data_dir`        | `PIRI_REPO_BLOB_DIR` | No |

## Fields

### `data_dir`

Directory for persistent data (databases, blobs, claims).

### `temp_dir`

Directory for temporary files during processing.

### `blob_dir`

Directory for blob bytes and IPNI adverts when blobs are stored on the
filesystem. These are the data the S3 backend keeps in its bucket.
Piri keeps them in `blob_dir/pdp/datastore` and `blob_dir/ipni`, the same
layout as under `data_dir`. Everything else stays in `data_dir`. Use it to put
blobs on a large storage array and the rest of Piri's state on faster drives.
Ignored when S3 is configured.

To split an existing node, stop Piri, set `blob_dir` to the old `data_dir`,
and move everything except `pdp/datastore` and `ipni` to the new `data_dir`.

## TOML

```toml
[repo]
data_dir = "/data/piri"
temp_dir = "/tmp/piri"
blob_dir = "/mnt/array/piri"
```

