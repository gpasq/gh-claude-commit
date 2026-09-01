# gh-claude-commit

A [GitHub CLI][gh] extension that writes your commit message for you. It hands
your staged diff to [Claude Code][cc] and prints a commit message that is ready
to use verbatim — no headers, no fences, no "Here's a commit message for you".

![gh claude-commit generating and confirming a commit message](docs/demo.gif)


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

Stage your changes, then:

```bash
gh claude-commit --commit
```

It shows the message and waits before anything is written:

```console
── proposed commit message ──────────────────────────────
Cache resolved config per workspace root

The resolver walked the full parent chain on every lookup.
────────────────────────────────────────────────────────────
Commit this message? [Y]es  [e]dit  [n]o:
```

Enter or `y` commits as-is, `e` opens the message in your `$EDITOR` first, and
`n` aborts with a non-zero exit — so `gh claude-commit --commit && git push`
will not push a commit you rejected.

### Print it instead

With no flags, the message goes to stdout and nothing touches your repository.
Useful for piping, or just for a look:

```bash
gh claude-commit
```

```bash
gh claude-commit | git commit -F -
```

### Go straight to the editor

Skips the prompt and opens `$EDITOR` with the message ready. Emptying the file
aborts the commit, exactly as with `git commit`:

```bash
gh claude-commit --edit
```

### Steer the message

Anything after the flags is passed to the model as extra guidance:

```bash
gh claude-commit --commit -- "emphasize that this fixes the flaky retry test"
```

### Amend the last commit

`--amend` describes the *amended* commit: it diffs the index against the parent
of `HEAD`, and shows Claude the message it is replacing.

```bash
gh claude-commit --amend --edit
```

### Choose a style

By default Claude sees the last ten commit subjects and is asked to match them,
so a repository already using Conventional Commits keeps using them. Force it
either way with `--conventional` or `--style plain`:

```bash
gh claude-commit --commit --conventional
```

### Inspect the prompt without calling Claude

Prints exactly what would be sent — instruction and context — and exits. Costs
nothing, and is the fastest way to see what the model is working from:

```bash
gh claude-commit --dry-run
```

### Non-interactive use

The confirmation prompt only appears when stdin and stderr are both terminals,
so hooks, pipelines and CI are never blocked by it. To skip it deliberately on a
terminal, pass `--yes`:

```bash
gh claude-commit --commit --yes
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

The demo at the top of this README is re-recordable — see
[`docs/demo.tape`](docs/demo.tape) for the [vhs](https://github.com/charmbracelet/vhs)
script and the crop command.

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
