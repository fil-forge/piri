# Generating and Managing DIDs & Cryptographic Keys

The `service.pem` file contains the key your storage node signs with. The node's DID comes from its public URL: a node at `https://piri.example.com` is `did:web:piri.example.com`, and it publishes this key in the DID document it serves at `/.well-known/did.json`.

## Prerequisites

Before generating keys, ensure you have:

- ✅ [Installed Piri](./installation.md)

## Key Requirements

Piri requires an **Ed25519** private key. Ed25519 is a modern elliptic curve signature scheme.

## Generating a PEM File & DID

**Step 1: Generate a new Ed25519 key**

```bash
piri identity generate > service.pem
```

**Step 2: Verify the key**

```bash
piri identity parse service.pem
```

Example output: `did:key:z6MkhaXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX`

This is the DID of the key itself. It is not the node's DID, which `piri init` prints once it knows the node's public URL.

## Security Considerations

- **Protect this file**: It contains your private key
- **Set appropriate file permissions**: `chmod 600 service.pem`
- **Backup securely**: The node cannot sign without it

## Preparing Your Wallet File

Export your Filecoin wallet address to a hex file:

```bash
# Export wallet from Lotus to hex format
lotus wallet export YOUR_DELEGATED_ADDRESS > wallet.hex
```

This wallet file will be used during the initialization process to import your wallet into Piri.

---

## Next Steps

After generating your keys:

- [Configure TLS Termination](./tls-termination.md)