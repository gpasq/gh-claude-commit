package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// findClaude locates the Claude Code CLI. $CLAUDE_BIN wins, then $PATH, then
// the paths the official installers use.
func findClaude(override string) (string, error) {
	candidates := []string{override, os.Getenv("CLAUDE_BIN")}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		path, err := exec.LookPath(c)
		if err != nil {
			return "", fmt.Errorf("claude CLI %q not found or not executable", c)
		}
		return path, nil
	}
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, p := range []string{
			filepath.Join(home, ".claude", "local", "claude"),
			filepath.Join(home, ".local", "bin", "claude"),
			filepath.Join(home, "bin", "claude"),
		} {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, nil
			}
		}
	}
	return "", errors.New("claude CLI not found on PATH\n" +
		"install Claude Code (https://claude.com/claude-code), or point this\n" +
		"extension at it with --claude-bin or the CLAUDE_BIN environment variable")
}

type claudeRequest struct {
	bin         string
	instruction string
	stdin       string
	model       string
	allowMCP    bool
	extraArgs   []string
	timeout     time.Duration
}

// buildArgs assembles the claude command line. The instruction goes last, as
// the positional prompt, while the diff itself is delivered on stdin.
func buildArgs(req claudeRequest) []string {
	args := []string{"--print", "--output-format", "text"}
	// Writing a commit message never needs MCP servers, and connecting to them
	// dominates the runtime when several are configured.
	if !req.allowMCP {
		args = append(args, "--strict-mcp-config")
	}
	if req.model != "" {
		args = append(args, "--model", req.model)
	}
	args = append(args, req.extraArgs...)
	return append(args, req.instruction)
}

// runClaude asks Claude Code for a commit message in headless print mode.
func runClaude(req claudeRequest) (string, error) {
	args := buildArgs(req)

	ctx, cancel := context.WithTimeout(context.Background(), req.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, req.bin, args...)
	cmd.Stdin = strings.NewReader(req.stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	stop := startSpinner("Asking Claude for a commit message")
	err := cmd.Run()
	stop()

	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("claude timed out after %s (raise it with --timeout)", req.timeout)
	}
	if err != nil {
		detail := strings.TrimSpace(errb.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("claude failed: %s", detail)
	}
	msg := cleanMessage(out.String())
	if msg == "" {
		return "", errors.New("claude returned an empty commit message")
	}
	return msg, nil
}

var (
	fenceOpen  = regexp.MustCompile("(?m)^[ \t]*```[a-zA-Z0-9_+-]*[ \t]*$")
	blankRuns  = regexp.MustCompile(`\n{3,}`)
	trailingWS = regexp.MustCompile(`(?m)[ \t]+$`)
)

// cleanMessage strips the wrappers a model sometimes adds around its answer so
// the result is usable as a commit message verbatim.
func cleanMessage(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSpace(s)

	// Unwrap a message that came back inside a single fenced code block.
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) > 1 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			s = strings.Join(lines[1:len(lines)-1], "\n")
		} else {
			s = fenceOpen.ReplaceAllString(s, "")
		}
		s = strings.TrimSpace(s)
	}

	// Unwrap a message the model quoted in its entirety.
	if len(s) > 1 && !strings.Contains(s, "\n") {
		if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
			(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}

	s = trailingWS.ReplaceAllString(s, "")
	s = blankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// startSpinner shows progress on stderr, leaving stdout clean for the message.
// It is a no-op when stderr is not a terminal.
func startSpinner(label string) func() {
	if !isTerminal(os.Stderr) {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		for i := 0; ; i++ {
			select {
			case <-done:
				fmt.Fprintf(os.Stderr, "\r\033[2K")
				return
			case <-time.After(80 * time.Millisecond):
				fmt.Fprintf(os.Stderr, "\r\033[2K%c %s", frames[i%len(frames)], label)
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
