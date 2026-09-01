# gh-claude-commit

A [GitHub CLI][gh] extension that writes your commit message for you. It hands
your staged diff to [Claude Code][cc] and prints a commit message that is ready
to use verbatim — no headers, no fences, no "Here's a commit message for you".

```console
$ git add -A
$ gh claude-commit
Add gh-claude-commit extension

A GitHub CLI extension that generates commit messages from staged
changes by piping the diff to Claude Code in headless print mode.

- Collect the staged diff, diffstat, branch name, and recent commit
  subjects, then send them to `claude --print` on stdin so the message
  can imitate the repository's prevailing style
- Budget oversized diffs by capping any single huge file before
  dropping whole files from the end, keeping the diffstat intact so
  the full scope of the change is still visible
- Strip code fences, wrapping quotes, and stray whitespace from the
  model's reply so the result is usable as a commit message verbatim
```

*(That is real output — it is the message this extension wrote for its own
initial commit, trimmed to the first few bullets.)*


## Requirements

- [GitHub CLI][gh] 2.0 or newer (`gh --version`)
- `git`, and a repository with staged changes
- [Claude Code][cc], installed and signed in — see below

## Install

```bash
gh extension install gpasq/gh-claude-commit
```

> This installs a prebuilt binary from the repository's latest release. If you
> are working from a fork or an unreleased clone, there is nothing to download
> yet — build and install the working copy instead (see
> [Development](#development)).

You also need [Claude Code][cc] installed and authenticated:

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

That puts `claude` in `~/.local/bin`. The extension looks on your `PATH` first,
then falls back to `~/.local/bin/claude`, `~/.claude/local/claude` and
`~/bin/claude`, so it works even if that directory isn't on your `PATH` yet. If
`claude` lives somewhere else entirely, point at it with `--claude-bin` or the
`CLAUDE_BIN` environment variable.

The desktop app does **not** provide the `claude` binary; you need the CLI.

Upgrade later with `gh extension upgrade claude-commit`, and remove it with
`gh extension remove claude-commit`.

## Usage

By default the message goes to stdout, so it pipes straight into a commit:

```bash
gh claude-commit | git commit -F -
```

Or let the extension make the commit itself. It shows you the message and waits
for an answer before anything is written:

```bash
gh claude-commit --commit
```

```console
── proposed commit message ──────────────────────────────
Cache resolved config per workspace root

The resolver walked the full parent chain on every lookup.
────────────────────────────────────────────────────────────
Commit this message? [Y]es  [e]dit  [n]o:
```

`e` opens the message in your `$EDITOR` before committing; `n` aborts and exits
non-zero, so `gh claude-commit --commit && git push` will not push. To skip
straight to the editor, use `--edit`:

```bash
gh claude-commit --edit
```

The prompt only appears when stdin and stderr are both terminals, so hooks,
pipelines and CI are never blocked by it. Pass `--yes` to skip it deliberately:

```bash
gh claude-commit --commit --yes
```

Anything you add after the flags is passed to the model as extra guidance:

```bash
gh claude-commit --edit -- "call out that this fixes the flaky retry test"
```

### Amending

`--amend` describes the *amended* commit: it diffs the index against the parent
of `HEAD`, and shows Claude the message you're replacing.

```bash
git add forgotten-file.go
gh claude-commit --amend --edit
```

### Style

By default Claude is shown the last ten commit subjects and asked to match the
repository's existing style, so a repo that already uses Conventional Commits
keeps using them. Force the matter either way with `--conventional` /
`--style plain`.

```bash
gh claude-commit --conventional
```

## Flags

| Flag | Description |
| --- | --- |
| `-c`, `--commit` | Create the commit with the generated message |
| `-e`, `--edit` | Create the commit, opening your editor first (implies `-c`) |
| `-y`, `--yes` | Commit without asking for confirmation |
| `--amend` | Amend the previous commit instead of creating one |
| `--no-verify` | Skip pre-commit and commit-msg hooks |
| `-s`, `--signoff` | Add a `Signed-off-by` trailer |
| `--coauthor` | Add a `Co-Authored-By: Claude` trailer |
| `--style STYLE` | `auto` (default), `conventional`, or `plain` |
| `--conventional` | Shorthand for `--style conventional` |
| `--model MODEL` | Model to pass to `claude` (default `sonnet`; `""` uses your `claude` default) |
| `--hint TEXT` | Extra guidance for the model |
| `-o`, `--output FILE` | Write the message to `FILE` instead of stdout |
| `--max-bytes N` | Truncate the diff sent to Claude (default `120000`) |
| `--timeout DUR` | Give up on `claude` after this long (default `2m`) |
| `--allow-mcp` | Let `claude` load your MCP servers (slower; rarely useful) |
| `--claude-bin PATH` | Path to the `claude` executable |
| `--claude-arg ARG` | Extra argument for `claude` (repeatable) |
| `--dry-run` | Print the prompt that would be sent, and exit |
| `--version` | Print the extension version |

## Recipes

A `git ai` alias:

```bash
git config --global alias.ai '!gh claude-commit --edit'
```

Use it as a `prepare-commit-msg` hook, so `git commit` opens with a draft
already written. Save this as `.git/hooks/prepare-commit-msg` and `chmod +x` it:

```bash
#!/bin/sh
# Only seed a message when the user didn't supply one (-m, -F, merges, amends).
[ -z "$2" ] || exit 0
gh claude-commit --output "$1" || true
```

## What a run costs

Whether a run costs money at all depends on how `claude` is authenticated:

- **Claude subscription (Pro/Max — the default OAuth login).** Nothing per run.
  Usage draws down your plan's rate limits, not a dollar balance. If you exceed
  your plan's included usage and have extra usage enabled, overflow is billed;
  otherwise you are simply rate limited until the window resets.
- **API billing (`ANTHROPIC_API_KEY` set).** Tokens are billed at list price and
  the figures below are real money.

Which mode you are in is decided by the environment the extension inherits, not
by anything it configures. If `ANTHROPIC_API_KEY` is exported in your shell — for
an SDK or another CLI — `claude` uses it and bills you per token, even though a
subscription is signed in. Nothing warns you about the switch. Check with:

```bash
env | grep ANTHROPIC_
```

`claude --output-format json` reports a `total_cost_usd` field either way. On a
subscription it is a *notional* number — what the same tokens would have cost on
pay-as-you-go API billing — not a charge. Don't read it as your bill.

Measured on this repository's ~35KB initial diff with `--model sonnet`:

| | Tokens | Notional cost |
| --- | --- | --- |
| Fixed Claude Code overhead | ~38,900 | $0.048 |
| The staged diff itself | ~22,500 | $0.092 |
| Generated message | ~350 | $0.005 |

The fixed overhead is Claude Code's system prompt, built-in tool definitions and
`CLAUDE.md`, reloaded on every headless invocation: a trivial six-word prompt
through the same code path still burns ~39,000 tokens. **A one-line commit
therefore costs nearly as much as a large one.** Roughly 88% of the total is the
cache *write* (charged at 1.25x), not the model reasoning.

There is no way to shed that overhead through the CLI — replacing the system
prompt with `--system-prompt` was measured and made things *worse*, because a
custom prompt loses the cached prefix. Talking to the Messages API directly would
avoid it, but that requires an API key and would turn a subscription-covered
operation into a metered one.

Timing: about 7-8s per message, most of it model latency.

### Whose account does it use?

Whoever installs it uses their own. The extension ships no API key, no endpoint
and no configuration — it shells out to the `claude` on your machine and that
CLI resolves the account itself, in this order: `ANTHROPIC_API_KEY` or
`ANTHROPIC_AUTH_TOKEN`, then Bedrock/Vertex/Foundry environment variables, then
an `apiKeyHelper` in your settings, then the OAuth credentials from your login.
Every installer needs their own authenticated Claude Code, and their runs draw
on their own plan or their own key.

Two consequences worth knowing before sharing this with a team:

- **CI cannot use a subscription.** OAuth login is interactive, so an Action or
  a bot needs `ANTHROPIC_API_KEY` — metered API usage billed to whoever owns
  that key.
- **The staged diff leaves the machine.** It is sent to Anthropic under the
  running user's own account and their organization's data settings. If that
  matters for your source, settle it before rolling this out.

### Why Sonnet is the default

Sonnet is the default because a commit message is a short, well-specified
summarization task. On API billing it is less than half the price of Opus; on a
subscription it consumes your plan's limits more slowly. An early version of the
prompt let Sonnet drift into describing the change file by file where Opus
summarized by behavior; tightening the "organize around behavior, not file
layout" rule closed that gap, and the two now produce comparable messages.

Pass `--model opus` for a larger model, or `--model ""` to defer to whatever
`claude` itself is configured with.

## How it works

The extension collects the staged diff (`git diff --cached`), the diffstat, the
current branch name and the last ten commit subjects, and sends them to
`claude --print --strict-mcp-config` on stdin. Large diffs are budgeted rather
than blindly cut off: any single oversized file (a lockfile, a generated bundle)
is capped first so it can't crowd out the real change, then whole files are
dropped from the end. The
diffstat always goes through intact, so the model still knows the full scope of
what it is describing.

Nothing is written to your repository unless you pass `--commit`, `--edit`, or
`--output` — and `--commit` asks first whenever it has a terminal to ask on.

## Development

```bash
go test ./...
go build -o gh-claude-commit .
gh extension install .          # install this working copy
```

`gh claude-commit --dry-run` prints the exact prompt without calling Claude,
which is the fastest way to iterate on the prompt itself.

### Releasing

`gh extension install owner/repo` downloads a prebuilt binary from a GitHub
release, so the install command at the top of this README only resolves once a
release exists. Publishing one:

```bash
git add -A
gh claude-commit --commit          # or write the message yourself
gh repo create gh-claude-commit --public --source=. --remote=origin --push
git tag v0.1.0
git push origin v0.1.0
```

The tag triggers [`.github/workflows/release.yml`](.github/workflows/release.yml),
which runs the tests and then uses [`cli/gh-extension-precompile`][precompile] to
cross-compile binaries for each platform and attach them to the release. Until
that finishes, the only way in is `gh extension install .` from a clone.

[precompile]: https://github.com/cli/gh-extension-precompile

## License

MIT

[gh]: https://cli.github.com
[cc]: https://claude.com/claude-code
