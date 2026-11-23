# parse

Parse a DID from a PEM file containing an Ed25519 key.

The output is the `did:key` of the key. A node's own DID is the `did:web` of its public URL; see [identity](../../configuration/identity.md#the-nodes-did).

## Usage

```
piri identity parse <pem-file>
```

## Arguments

| Argument | Description |
|----------|-------------|
| `<pem-file>` | Path to PEM file containing Ed25519 private key |

## Example

```bash
piri identity parse my-key.pem
```

```
# did:key:z6MkhaXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
```
