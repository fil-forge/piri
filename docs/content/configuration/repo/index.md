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

### `s3`

S3-compatible object storage for blob bytes and IPNI adverts, in place of
`blob_dir`. Set `endpoint` to the host of the storage API, with a port if it
is not 443, and `bucket_prefix` to where the objects go:

- A prefix without a `/` names a bucket of its own: `piri-` keeps blobs in the
  bucket `piri-pdp` and IPNI adverts under `ipni/` in that bucket.
- A prefix with a `/` uses a bucket shared with other data. The part before the
  first `/` names the bucket and the rest is a folder in it, which ends with
  `/`: `fil-one/piri-0/` keeps blobs under `piri-0/pdp/` and IPNI adverts under
  `piri-0/ipni/` in the bucket `fil-one`. `fil-one/` keeps them under `pdp/`
  and `ipni/`.

Piri creates the bucket on its first start when it does not exist. Give each
Piri a prefix of its own. Piri uses HTTPS unless `insecure` is set, which is
for development only.

## TOML

```toml
[repo]
data_dir = "/data/piri"
temp_dir = "/tmp/piri"
blob_dir = "/mnt/array/piri"
```

With blobs in S3:

```toml
[repo.s3]
endpoint = "objects.example.com"
bucket_prefix = "fil-one/piri-0/"

[repo.s3.credentials]
access_key_id = "YOUR_ACCESS_KEY_ID"
secret_access_key = "YOUR_SECRET_ACCESS_KEY"
```

