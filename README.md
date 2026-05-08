# git-account

A small CLI for managing multiple Git identities (GitHub, GitLab, Bitbucket) on a single machine. It generates a dedicated SSH key per account, wires up `~/.ssh/config` with a host alias, and switches your global `user.name` / `user.email` on demand.

## Why

If you push to more than one Git host — personal GitHub, a work GitLab, a client's Bitbucket — you've probably hit the "wrong author email on a commit" or "permission denied (publickey)" dance. `git-account` keeps each identity isolated (its own key, its own host alias) and lets you switch between them with one command.

## Install

Requires Go 1.24+ and `ssh-keygen` / `ssh-add` on your `PATH`.

```sh
git clone <this-repo> git-account-cli
cd git-account-cli
go build -o git-account .
mv git-account /usr/local/bin/   # or anywhere on your PATH
```

## Usage

```
git-account <command> [args]
```

| Command | Description |
| --- | --- |
| `add <name> <email> <type>` | Create a new account. `type` is `github`, `gitlab`, or `bitbucket`. Generates a key, updates `~/.ssh/config`, and prints next steps for uploading the public key. |
| `switch <name>` | Set the global `user.name` / `user.email` and load this account's key into `ssh-agent`. |
| `use <name> [path]` | Bind a directory (default: cwd) and its subdirectories to an account. Inside the tree, git auto-applies that account's identity and SSH key. |
| `unuse [path]` | Remove the binding for a directory (default: cwd). |
| `list` | Show all configured accounts, directory bindings, and the current global Git identity. |
| `remove <name>` | Delete the account: SSH key pair, SSH config block, saved metadata, and any directory bindings pointing at it. |
| `help` | Print the help text. |

### Example: add a personal GitHub and a work Bitbucket

```sh
git-account add personal_github me@example.com github
git-account add work_bitbucket  me@company.com bitbucket
```

After running `add`, follow the printed instructions to paste the public key into the Git host's SSH key settings.

### Cloning under a specific account

`add` creates a host alias of the form `<host>-<name>` (e.g. `github.com-personal_github`, `bitbucket.org-work_bitbucket`). Use that alias in your clone URL:

```sh
git clone git@github.com-personal_github:me/my-repo.git
git clone git@bitbucket.org-work_bitbucket:company/internal.git
```

The host alias is what tells SSH which key to use, so the right identity is picked up automatically.

### Switching the global commit identity

```sh
git-account switch personal_github
```

This updates `~/.gitconfig`'s `user.name` / `user.email` and adds the account's key to `ssh-agent`. Per-repo overrides via `git config user.email …` still work as usual.

### Per-directory accounts

If you keep work in `~/code/work` and personal projects in `~/code/personal`, you can bind each tree to an account once and forget about it:

```sh
git-account use work_bitbucket ~/code/work
git-account use personal_github ~/code/personal
```

Any repo under those paths automatically uses the right `user.name`, `user.email`, and SSH key — no `switch` required, and plain `git@github.com:…` clone URLs work without the host-alias prefix because `core.sshCommand` is pinned per directory.

Bindings are implemented as native git `includeIf "gitdir:<path>/"` entries in `~/.gitconfig` pointing at a per-account include file (`~/.git_accounts_config/<name>.gitconfig`), so anything that reads git config — including IDEs and other tools — picks up the right identity.

To remove a binding:

```sh
git-account unuse ~/code/work
```

`git-account list` shows every active binding.

## What it touches

- `~/.ssh/id_ed25519_<name>` and `.pub` — generated key pair (no passphrase).
- `~/.ssh/config` — adds a `Host <alias>` block with `IdentitiesOnly yes`.
- `~/.git_accounts_config/<name>.ini` — saved account metadata.
- `~/.git_accounts_config/<name>.gitconfig` — per-account include file with `user.*` and `core.sshCommand` (created on `use`).
- `~/.gitconfig` — `user.name` / `user.email` (on `switch`); `[includeIf "gitdir:…"]` blocks (on `use`).

`remove` cleans up the SSH key, SSH config block, account metadata, include file, and any directory bindings pointing at the account. It does not revert the global `user.name` / `user.email`.

## Notes

- Keys are generated with `ssh-keygen -t ed25519 -N ""` (no passphrase) for convenience. If you'd rather use a passphrase, generate the key yourself and edit the `.ini` to point at it.
- `add` won't overwrite an existing key file with the same name — it reuses it.
- The tool assumes standard hostnames (`github.com`, `gitlab.com`, `bitbucket.org`). Self-hosted instances aren't supported out of the box.
