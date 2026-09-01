package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// git runs a git command and returns its stdout.
func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

func gitOK(args ...string) bool {
	_, err := git(args...)
	return err == nil
}

func requireRepo() error {
	if !gitOK("rev-parse", "--git-dir") {
		return errors.New("not inside a git repository")
	}
	return nil
}

// emptyTree is the well-known hash of git's empty tree object, used as the
// diff base when there is no commit to compare against yet.
func emptyTree() string {
	if out, err := git("hash-object", "-t", "tree", "/dev/null"); err == nil {
		return strings.TrimSpace(out)
	}
	return "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
}

// diffBase returns the commit the index should be compared against. For an
// amend that is the parent of HEAD, since the amended commit replaces HEAD.
func diffBase(amend bool) string {
	ref := "HEAD"
	if amend {
		ref = "HEAD^"
	}
	if gitOK("rev-parse", "--verify", "--quiet", ref+"^{commit}") {
		return ref
	}
	return emptyTree()
}

func stagedDiff(base string) (string, error) {
	return git("diff", "--cached", "--no-color", "--no-ext-diff", "-U3", base)
}

func stagedStat(base string) (string, error) {
	return git("diff", "--cached", "--no-color", "--stat=200,160", base)
}

// hasWorktreeChanges reports whether anything at all is modified, so we can
// tell "nothing to commit" apart from "you forgot to stage".
func hasWorktreeChanges() bool {
	out, err := git("status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

func branchName() string {
	out, err := git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	if b := strings.TrimSpace(out); b != "HEAD" {
		return b
	}
	return ""
}

// recentSubjects returns the subject lines of recent commits, which give the
// model the repository's prevailing commit style to imitate.
func recentSubjects(n int, amend bool) []string {
	ref := "HEAD"
	if amend {
		ref = "HEAD^"
	}
	if !gitOK("rev-parse", "--verify", "--quiet", ref+"^{commit}") {
		return nil
	}
	out, err := git("log", "--no-merges", "--pretty=format:%s", fmt.Sprintf("-n%d", n), ref)
	if err != nil {
		return nil
	}
	var subs []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			subs = append(subs, l)
		}
	}
	return subs
}

// amendedMessage is the message of the commit being amended, useful context
// when the author is reworking an existing commit.
func amendedMessage() string {
	out, err := git("log", "-1", "--pretty=format:%B")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// commit runs `git commit -F <file>` with stdio attached so hooks, editors and
// GPG prompts behave exactly as they would for a hand-written commit.
func commit(msgFile string, extra []string) error {
	args := append([]string{"commit", "-F", msgFile}, extra...)
	cmd := exec.Command("git", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
