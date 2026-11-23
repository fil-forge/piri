# server

HTTP server configuration.

| Key                 | Default                | Env                      | Dynamic |
|---------------------|------------------------|--------------------------|---------|
| `server.port`       | `3000`                 | `PIRI_SERVER_PORT`       | No      |
| `server.host`       | `0.0.0.0`              | `PIRI_SERVER_HOST`       | No      |
| `server.public_url` | -                      | `PIRI_SERVER_PUBLIC_URL` | No      |

## Fields

### `port`

HTTP listening port (1-65535).

### `host`

HTTP listening host/interface.

### `public_url`

Externally accessible URL. Required: the node does not start without it.

The node's [DID](identity.md#the-nodes-did) is derived from this URL, so it has
to be the root of a host, such as `https://piri.example.com`. A URL with a path,
query or fragment is refused. A port other than the scheme's default becomes
part of the DID.

The URL has to be `https`. Other services fetch the node's DID document over
HTTPS, so a node at an `http` URL could not be identified. Development stacks
that resolve DIDs over HTTP set `ucan.insecure_did_resolution`, which allows an
`http` URL.

## TOML

```toml
[server]
port = 3000
host = "0.0.0.0"
public_url = "https://piri.example.com"
```
