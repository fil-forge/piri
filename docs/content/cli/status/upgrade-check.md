# upgrade-check

Check if it's safe to upgrade the node.

## Usage

```
piri status upgrade-check
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Safe to upgrade |
| `1` | Not safe to upgrade |
| `2` | Unable to determine status |

## Example

```bash
piri status upgrade-check && piri update
```

```
Safe to upgrade
```

When the node is not safe to upgrade, the command prints the reason to stderr and exits with code `1`. The reason names the blocking condition:

| Condition | Message |
|-----------|---------|
| Proof set missed its challenge window (fault state) | `Not safe to upgrade: proof set <id> is in a fault state: challenge window opened at epoch <next challenge epoch> (next challenge epoch) and closed at epoch <window end> without a proof; current epoch is <current epoch>` |
| Node is generating a proof | `Not safe to upgrade: node is currently generating a proof for proof set <id> (current epoch <current epoch>); wait for it to finish` |
| Challenge window open, no proof submitted yet | `Not safe to upgrade: proof set <id> is in a challenge window and has not submitted a proof yet: window opened at epoch <next challenge epoch> (next challenge epoch) and closes at epoch <window end>; current epoch is <current epoch>` |

This command is designed for use in scripts and automation. It returns exit codes to indicate whether an upgrade can safely proceed without interrupting proof generation.
