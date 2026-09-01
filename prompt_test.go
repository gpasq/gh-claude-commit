package main

import (
	"strings"
	"testing"
)

func TestCleanMessage(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Add retry to the uploader\n", "Add retry to the uploader"},
		{"fenced", "```\nFix the parser\n\nIt choked on empty input.\n```", "Fix the parser\n\nIt choked on empty input."},
		{"fenced with language", "```text\nFix the parser\n```\n", "Fix the parser"},
		{"quoted single line", "\"Bump the client to v2\"", "Bump the client to v2"},
		{"crlf and blank runs", "Subject\r\n\r\n\r\n\r\nBody", "Subject\n\nBody"},
		{"trailing whitespace", "Subject   \n\nBody\t\n", "Subject\n\nBody"},
		{"quotes inside a body are kept", "Rename \"foo\"\n\nBecause \"foo\" was vague.", "Rename \"foo\"\n\nBecause \"foo\" was vague."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanMessage(c.in); got != c.want {
				t.Errorf("cleanMessage(%q)\n got: %q\nwant: %q", c.in, got, c.want)
			}
		})
	}
}

func fakeDiff(file string, lines int) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + file + " b/" + file + "\n")
	b.WriteString("--- a/" + file + "\n+++ b/" + file + "\n@@ -1 +1 @@\n")
	for i := 0; i < lines; i++ {
		b.WriteString("+some added line of content here\n")
	}
	return b.String()
}

func TestBudgetDiffUnderLimit(t *testing.T) {
	d := fakeDiff("a.go", 10)
	got, truncated := budgetDiff(d, 120000)
	if truncated || got != d {
		t.Fatalf("small diff should pass through untouched (truncated=%v)", truncated)
	}
}

func TestBudgetDiffCapsHugeFile(t *testing.T) {
	// A generated lockfile must not crowd the real change out of the prompt.
	d := fakeDiff("lock.json", 20000) + fakeDiff("real.go", 5)
	got, truncated := budgetDiff(d, 60000)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(got) > 60000 {
		t.Fatalf("budget exceeded: %d bytes", len(got))
	}
	if !strings.Contains(got, "b/real.go") {
		t.Error("the smaller file was dropped; it should have survived")
	}
	if !strings.Contains(got, "truncated") {
		t.Error("expected a truncation marker in the output")
	}
}

func TestBudgetDiffDropsWholeFiles(t *testing.T) {
	var d strings.Builder
	for _, f := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		d.WriteString(fakeDiff(f, 400))
	}
	got, truncated := budgetDiff(d.String(), 20000)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(got, "changed files omitted from the diff") {
		t.Error("expected a note about omitted files")
	}
}

func TestSplitDiffByFile(t *testing.T) {
	d := fakeDiff("a.go", 2) + fakeDiff("b.go", 2)
	chunks := splitDiffByFile(d)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if !strings.HasPrefix(chunks[1], "diff --git a/b.go") {
		t.Errorf("second chunk starts wrong: %q", chunks[1][:20])
	}
}

func TestBuildInstructionStyles(t *testing.T) {
	if !strings.Contains(buildInstruction("conventional", ""), "Conventional Commit") {
		t.Error("conventional style not applied")
	}
	if strings.Contains(buildInstruction("plain", ""), "Conventional Commit") {
		t.Error("plain style should not mention Conventional Commits")
	}
	if !strings.Contains(buildInstruction("auto", ""), "Match the tone") {
		t.Error("auto style should ask the model to match recent commits")
	}
	if !strings.Contains(buildInstruction("auto", "mention the retry loop"), "mention the retry loop") {
		t.Error("hint not included")
	}
}

func TestBuildContextOmitsEmptySections(t *testing.T) {
	out := buildContext(&contextInput{diff: fakeDiff("a.go", 2), maxBytes: 1000})
	for _, tag := range []string{"<branch>", "<recent-commit-subjects>", "<diffstat>", "<message-being-amended>"} {
		if strings.Contains(out, tag) {
			t.Errorf("unexpected %s for empty input", tag)
		}
	}
	if !strings.Contains(out, "<staged-diff>") {
		t.Error("missing staged diff section")
	}
}

func TestParseOptionsHintFromArgs(t *testing.T) {
	o, err := parseOptions([]string{"--edit", "focus on", "the retry loop"})
	if err != nil {
		t.Fatal(err)
	}
	if o.hint != "focus on the retry loop" {
		t.Errorf("hint = %q", o.hint)
	}
	if !o.commit {
		t.Error("--edit should imply --commit")
	}
}

func TestParseOptionsRejectsUnknownStyle(t *testing.T) {
	if _, err := parseOptions([]string{"--style", "haiku"}); err == nil {
		t.Error("expected an error for an unknown style")
	}
}

func TestBuildArgs(t *testing.T) {
	got := strings.Join(buildArgs(claudeRequest{instruction: "INSTR"}), " ")
	want := "--print --output-format text --strict-mcp-config INSTR"
	if got != want {
		t.Errorf("defaults:\n got: %s\nwant: %s", got, want)
	}

	got = strings.Join(buildArgs(claudeRequest{instruction: "INSTR", allowMCP: true}), " ")
	if strings.Contains(got, "--strict-mcp-config") {
		t.Errorf("--allow-mcp should drop --strict-mcp-config, got: %s", got)
	}

	got = strings.Join(buildArgs(claudeRequest{
		instruction: "INSTR",
		model:       "sonnet",
		extraArgs:   []string{"--verbose"},
	}), " ")
	want = "--print --output-format text --strict-mcp-config --model sonnet --verbose INSTR"
	if got != want {
		t.Errorf("model and passthrough:\n got: %s\nwant: %s", got, want)
	}
}

func TestDefaultModelIsSonnet(t *testing.T) {
	o, err := parseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.model != "sonnet" {
		t.Errorf("default model = %q, want sonnet", o.model)
	}
	if args := strings.Join(buildArgs(claudeRequest{model: o.model}), " "); !strings.Contains(args, "--model sonnet") {
		t.Errorf("default args missing model: %s", args)
	}
}

func TestEmptyModelDefersToClaudeConfig(t *testing.T) {
	o, err := parseOptions([]string{"--model", ""})
	if err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(buildArgs(claudeRequest{model: o.model}), " "); strings.Contains(args, "--model") {
		t.Errorf(`--model "" should omit the flag entirely, got: %s`, args)
	}
}
