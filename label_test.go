package main

import "testing"

func TestSessionTitle(t *testing.T) {
	for title, want := range map[string]string{
		"herder-plugin-rename":       "herder-plugin-rename",
		"Claude startship prompt":    "Claude startship prompt",
		"  Luvus documentation  ":    "Luvus documentation",
		"":                           "",
		"claude":                     "",
		"Claude Code":                "",
		"zsh":                        "",
		"zeeshans@mbp:~/projects":    "",
		"zeeshans@mbp ~/projects":    "",
		"~/projects/personal":        "",
		"/Users/zeeshans":            "",
		"fix ~/projects build":       "fix ~/projects build",
		"email bob@example.com spec": "email bob@example.com spec",
	} {
		if got := sessionTitle(&agent{Agent: "claude", TerminalTitle: title}); got != want {
			t.Errorf("sessionTitle(%q) = %q, want %q", title, got, want)
		}
	}
	if got := sessionTitle(&agent{Agent: "codex", TerminalTitle: "Codex"}); got != "" {
		t.Errorf("agent's own name: got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Errorf("got %q", got)
	}
	if got := truncate("abc", 4); got != "abc" {
		t.Errorf("got %q", got)
	}
}
