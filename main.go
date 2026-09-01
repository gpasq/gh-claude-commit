package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

const coauthorTrailer = "Co-Authored-By: Claude <noreply@anthropic.com>"

// defaultModel keeps the common case light. A commit message is a short,
// well-specified summarization task, so a larger model is not worth the extra
// draw on an API bill or on a subscription's rate limits. Pass --model "" to
// fall back to whatever claude itself is configured to use.
const defaultModel = "sonnet"

// errHelp signals that usage was printed and there is nothing left to do.
var errHelp = errors.New("help requested")

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, " ") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type options struct {
	commit     bool
	edit       bool
	amend      bool
	noVerify   bool
	signoff    bool
	coauthor   bool
	style      string
	model      string
	hint       string
	output     string
	maxBytes   int
	timeout    time.Duration
	allowMCP   bool
	claudeBin  string
	claudeArgs stringList
	dryRun     bool
	showVer    bool
}

func main() {
	err := run()
	if errors.Is(err, errHelp) {
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gh claude-commit: %s\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `gh claude-commit - write a commit message from your staged changes with Claude

Usage:
  gh claude-commit [flags] [hint...]

By default the message is printed to stdout, so it can be piped straight into a
commit:

  gh claude-commit | git commit -F -

Or let the extension make the commit itself:

  gh claude-commit --commit
  gh claude-commit --edit          # opens your editor with the message ready

Any trailing arguments are extra guidance for the model, e.g.
  gh claude-commit --edit -- "mention that this fixes the retry loop"

Flags:
  -c, --commit           Create the commit with the generated message
  -e, --edit             Create the commit, opening your editor first (implies -c)
      --amend            Amend the previous commit instead of creating one
      --no-verify        Skip pre-commit and commit-msg hooks
  -s, --signoff          Add a Signed-off-by trailer
      --coauthor         Add a Co-Authored-By trailer crediting Claude
      --style STYLE      auto (default), conventional, or plain
      --conventional     Shorthand for --style conventional
      --model MODEL      Model to pass to claude (default sonnet; pass an empty
                         string to use whatever claude is configured with)
      --hint TEXT        Extra guidance for the model
  -o, --output FILE      Write the message to FILE instead of stdout
      --max-bytes N      Truncate the diff sent to Claude (default 120000)
      --timeout DUR      Give up on claude after this long (default 2m)
      --allow-mcp        Let claude load your MCP servers (slower; rarely useful)
      --claude-bin PATH  Path to the claude executable
      --claude-arg ARG   Extra argument for claude (repeatable)
      --dry-run          Print the prompt that would be sent, and exit
      --version          Print the extension version
`)
}

func parseOptions(argv []string) (*options, error) {
	o := &options{}
	fs := flag.NewFlagSet("gh-claude-commit", flag.ContinueOnError)
	fs.Usage = usage
	fs.SetOutput(os.Stderr)

	var conventional bool
	for _, name := range []string{"commit", "c"} {
		fs.BoolVar(&o.commit, name, false, "")
	}
	for _, name := range []string{"edit", "e"} {
		fs.BoolVar(&o.edit, name, false, "")
	}
	for _, name := range []string{"signoff", "s"} {
		fs.BoolVar(&o.signoff, name, false, "")
	}
	for _, name := range []string{"output", "o"} {
		fs.StringVar(&o.output, name, "", "")
	}
	fs.BoolVar(&o.amend, "amend", false, "")
	fs.BoolVar(&o.noVerify, "no-verify", false, "")
	fs.BoolVar(&o.coauthor, "coauthor", false, "")
	fs.StringVar(&o.style, "style", "auto", "")
	fs.BoolVar(&conventional, "conventional", false, "")
	fs.StringVar(&o.model, "model", defaultModel, "")
	fs.StringVar(&o.hint, "hint", "", "")
	fs.IntVar(&o.maxBytes, "max-bytes", 120000, "")
	fs.DurationVar(&o.timeout, "timeout", 2*time.Minute, "")
	fs.BoolVar(&o.allowMCP, "allow-mcp", false, "")
	fs.StringVar(&o.claudeBin, "claude-bin", "", "")
	fs.Var(&o.claudeArgs, "claude-arg", "")
	fs.BoolVar(&o.dryRun, "dry-run", false, "")
	fs.BoolVar(&o.showVer, "version", false, "")

	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, errors.New("run `gh claude-commit --help` for usage")
	}
	if conventional {
		o.style = "conventional"
	}
	switch o.style {
	case "auto", "conventional", "plain":
	default:
		return nil, fmt.Errorf("unknown --style %q (want auto, conventional, or plain)", o.style)
	}
	if rest := strings.TrimSpace(strings.Join(fs.Args(), " ")); rest != "" {
		if o.hint == "" {
			o.hint = rest
		} else {
			o.hint += "\n" + rest
		}
	}
	if o.edit {
		o.commit = true
	}
	return o, nil
}

func run() error {
	o, err := parseOptions(os.Args[1:])
	if err != nil {
		return err
	}
	if o.showVer {
		fmt.Printf("gh-claude-commit %s\n", version)
		return nil
	}
	if err := requireRepo(); err != nil {
		return err
	}

	base := diffBase(o.amend)
	diff, err := stagedDiff(base)
	if err != nil {
		return err
	}
	if strings.TrimSpace(diff) == "" {
		return noChangesError(o.amend)
	}
	stat, err := stagedStat(base)
	if err != nil {
		return err
	}

	ctxIn := &contextInput{
		branch:   branchName(),
		subjects: recentSubjects(10, o.amend),
		stat:     stat,
		diff:     diff,
		maxBytes: o.maxBytes,
	}
	if o.amend {
		ctxIn.amendMsg = amendedMessage()
	}

	instruction := buildInstruction(o.style, o.hint)
	body := buildContext(ctxIn)

	if o.dryRun {
		fmt.Printf("--- instruction ---\n%s\n\n--- stdin ---\n%s", instruction, body)
		return nil
	}

	bin, err := findClaude(o.claudeBin)
	if err != nil {
		return err
	}
	msg, err := runClaude(claudeRequest{
		bin:         bin,
		instruction: instruction,
		stdin:       body,
		model:       o.model,
		allowMCP:    o.allowMCP,
		extraArgs:   o.claudeArgs,
		timeout:     o.timeout,
	})
	if err != nil {
		return err
	}
	if o.coauthor && !strings.Contains(msg, coauthorTrailer) {
		msg += "\n\n" + coauthorTrailer
	}
	msg += "\n"

	if o.output != "" {
		if err := os.WriteFile(o.output, []byte(msg), 0o644); err != nil {
			return err
		}
		if !o.commit {
			return nil
		}
	}
	if !o.commit {
		_, err := os.Stdout.WriteString(msg)
		return err
	}
	return commitWith(msg, o)
}

func noChangesError(amend bool) error {
	if amend {
		return errors.New("the commit being amended has no changes to describe")
	}
	if hasWorktreeChanges() {
		return errors.New("no staged changes\nstage what you want to commit first, e.g. `git add -p`")
	}
	return errors.New("no staged changes; the working tree is clean")
}

func commitWith(msg string, o *options) error {
	dir, err := os.MkdirTemp("", "gh-claude-commit")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "COMMIT_EDITMSG")
	if err := os.WriteFile(path, []byte(msg), 0o600); err != nil {
		return err
	}

	var args []string
	if o.amend {
		args = append(args, "--amend")
	}
	if o.noVerify {
		args = append(args, "--no-verify")
	}
	if o.signoff {
		args = append(args, "--signoff")
	}
	if o.edit {
		args = append(args, "--edit")
	}
	if err := commit(path, args); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	return nil
}
