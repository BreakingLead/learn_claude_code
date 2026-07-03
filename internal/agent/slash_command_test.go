package agent

import "testing"

func TestParseSlashCommand(t *testing.T) {
	name, args, ok := parseSlashCommand(" /debug now ")
	if !ok {
		t.Fatal("expected slash command")
	}
	if name != "debug" || args != "now" {
		t.Fatalf("unexpected command: name=%q args=%q", name, args)
	}

	name, args, ok = parseSlashCommand("/help\tverbose\nmode")
	if !ok {
		t.Fatal("expected slash command with whitespace-separated args")
	}
	if name != "help" || args != "verbose\nmode" {
		t.Fatalf("unexpected whitespace command: name=%q args=%q", name, args)
	}

	_, _, ok = parseSlashCommand("hello /debug")
	if ok {
		t.Fatal("expected normal message to skip command parsing")
	}
}

func TestSlashCommandRegistryCompletesByPrefix(t *testing.T) {
	registry := newSlashCommandRegistry()
	candidates := registry.complete("de")
	if len(candidates) != 1 {
		t.Fatalf("expected one candidate, got %d", len(candidates))
	}
	if candidates[0].Name != "debug" {
		t.Fatalf("unexpected candidate: %+v", candidates[0])
	}
}

func TestSlashCommandPrefixRequiresLeadingSlash(t *testing.T) {
	prefix, ok := slashCommandPrefix("  /he")
	if !ok || prefix != "he" {
		t.Fatalf("unexpected prefix: %q ok=%v", prefix, ok)
	}

	_, ok = slashCommandPrefix("please run /help")
	if ok {
		t.Fatal("expected embedded slash text to remain a normal message")
	}
}
