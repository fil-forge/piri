# identity

Node identity configuration.

| Key                  | Default | Env                      | Dynamic |
|----------------------|---------|--------------------------|---------|
| `identity.key_file`  | -       | `PIRI_IDENTITY_KEY_FILE` | No      |

## The node's DID

A node is identified by a `did:web` taken from its [public URL](server.md#public_url):
a node at `https://piri.example.com` is `did:web:piri.example.com`. The DID is
derived, so there is nothing to configure and it cannot disagree with where the
node is reached.

The node serves its DID document at `/.well-known/did.json`. Other services
fetch it to learn the key the node signs with, so the path must reach the node
through any proxy in front of it.

The key in `key_file` is the node's signing key. The node's identity is its
public URL: changing the URL makes it a different node to the network, which
has to be registered again.

## Fields

### `key_file`

Path to ED25519 PEM private key file. Generate with `piri identity generate`.

## TOML

```toml
[identity]
key_file = "/etc/piri/service.pem"
```
