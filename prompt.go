package main

import (
	"fmt"
	"strings"
)

const instructionBase = `Write a git commit message for the staged changes described on stdin.

Output rules:
- Output ONLY the commit message text. No preamble, no sign-off, no explanation,
  no markdown code fences, no surrounding quotes.
- Line 1 is the subject: imperative mood ("Add", not "Added"/"Adds"), at most 72
  characters, capitalized, no trailing period.
- If the change is small and self-explanatory, the subject line alone is enough.
- Otherwise leave line 2 blank, then write a body wrapped at 72 characters that
  explains what changed and why. Use "- " bullets when there are several
  independent changes.

Content rules:
- Describe only what the diff actually shows. Never invent motivation, ticket
  numbers, issue links, or effects you cannot see in the changes.
- Organize the message around behavior, not around the file layout. Never write
  one bullet per file, and never lead a bullet with a file name or path. A
  reader should learn what the change does, not which files it touched; the
  diffstat already records that.
- Group changes that serve one purpose into a single bullet, even when they are
  spread across several files.
- Do not restate line counts, and do not mention that the message was generated
  by an AI.`

const styleConventional = `
Format the subject as a Conventional Commit: "type(scope): description", where
type is one of feat, fix, docs, style, refactor, perf, test, build, ci, chore,
revert. Use a scope only when there is an obvious one. Keep the description in
lowercase imperative mood.`

const styleMatch = `
Match the tone, capitalization and formatting conventions of the recent commit
subjects shown in the context (including whether they use Conventional Commit
prefixes). If there are no recent commits, use the default style above.`

// buildInstruction assembles the prompt passed to claude as its query. The diff
// itself travels on stdin so it never has to fit in an argv-sized buffer.
func buildInstruction(style, hint string) string {
	var b strings.Builder
	b.WriteString(instructionBase)
	switch style {
	case "conventional":
		b.WriteString("\n")
		b.WriteString(styleConventional)
	case "plain":
		// The base rules already describe plain style.
	default:
		b.WriteString("\n")
		b.WriteString(styleMatch)
	}
	if hint = strings.TrimSpace(hint); hint != "" {
		b.WriteString("\n\nAdditional guidance from the author, which takes priority:\n")
		b.WriteString(hint)
	}
	return b.String()
}

type contextInput struct {
	branch    string
	subjects  []string
	stat      string
	diff      string
	amendMsg  string
	maxBytes  int
	truncated bool
}

// buildContext renders the repository context sent to claude on stdin.
func buildContext(in *contextInput) string {
	var b strings.Builder
	if in.branch != "" {
		fmt.Fprintf(&b, "<branch>%s</branch>\n\n", in.branch)
	}
	if len(in.subjects) > 0 {
		b.WriteString("<recent-commit-subjects>\n")
		for _, s := range in.subjects {
			b.WriteString(s)
			b.WriteString("\n")
		}
		b.WriteString("</recent-commit-subjects>\n\n")
	}
	if in.amendMsg != "" {
		b.WriteString("<message-being-amended>\n")
		b.WriteString(in.amendMsg)
		b.WriteString("\n</message-being-amended>\n\n")
	}
	if stat := strings.TrimRight(in.stat, "\n"); stat != "" {
		b.WriteString("<diffstat>\n")
		b.WriteString(stat)
		b.WriteString("\n</diffstat>\n\n")
	}
	diff, truncated := budgetDiff(in.diff, in.maxBytes)
	in.truncated = truncated
	b.WriteString("<staged-diff>\n")
	b.WriteString(strings.TrimRight(diff, "\n"))
	b.WriteString("\n</staged-diff>\n")
	if truncated {
		b.WriteString("\nThe diff above was truncated because it is large. The diffstat lists" +
			" every changed file, so summarize the change as a whole rather than only" +
			" the hunks shown.\n")
	}
	return b.String()
}

// splitDiffByFile breaks a unified diff into one chunk per file.
func splitDiffByFile(diff string) []string {
	lines := strings.Split(diff, "\n")
	var chunks []string
	var cur strings.Builder
	for _, l := range lines {
		if strings.HasPrefix(l, "diff --git ") && cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
		cur.WriteString(l)
		cur.WriteString("\n")
	}
	if strings.TrimSpace(cur.String()) != "" {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

// budgetDiff trims a diff to fit maxBytes. A single huge file (a lockfile, a
// generated bundle) is capped first so it cannot crowd out every other file,
// then whole files are dropped from the end if the total still does not fit.
func budgetDiff(diff string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(diff) <= maxBytes {
		return diff, false
	}
	perFile := maxBytes / 4
	if perFile < 4000 {
		perFile = 4000
	}

	chunks := splitDiffByFile(diff)
	truncated := false
	var b strings.Builder
	used := 0
	kept := 0
	for _, c := range chunks {
		if len(c) > perFile {
			c = clipAtLine(c, perFile) + "... [diff for this file truncated]\n"
			truncated = true
		}
		if used+len(c) > maxBytes && kept > 0 {
			truncated = true
			break
		}
		b.WriteString(c)
		used += len(c)
		kept++
	}
	if kept < len(chunks) {
		fmt.Fprintf(&b, "... [%d of %d changed files omitted from the diff]\n", len(chunks)-kept, len(chunks))
		truncated = true
	}
	return b.String(), truncated
}

// clipAtLine cuts s to at most n bytes, backing up to the last line break so
// the result is still a well-formed sequence of diff lines.
func clipAtLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[:i+1]
	}
	return s + "\n"
}
