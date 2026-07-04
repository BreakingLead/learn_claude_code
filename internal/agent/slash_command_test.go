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

func TestSlashCommandRegistryIncludesMode(t *testing.T) {
	registry := newSlashCommandRegistry()
	command, ok := registry.get("mode")
	if !ok {
		t.Fatal("expected /mode command")
	}
	if command.Usage != "/mode [name]" {
		t.Fatalf("unexpected mode usage: %q", command.Usage)
	}
}

func TestSlashCommandRegistryIncludesSessionCommands(t *testing.T) {
	registry := newSlashCommandRegistry()
	for _, name := range []string{"new", "resume"} {
		command, ok := registry.get(name)
		if !ok {
			t.Fatalf("expected /%s command", name)
		}
		if command.Usage == "" || command.Description == "" {
			t.Fatalf("expected command metadata for /%s: %+v", name, command)
		}
	}
}

func TestResolveSessionChoice(t *testing.T) {
	records := []sessionRecord{{ID: "sess_first"}, {ID: "sess_second"}}
	if got := resolveSessionChoice("1", records); got != "sess_first" {
		t.Fatalf("expected first session, got %q", got)
	}
	if got := resolveSessionChoice("sess_manual", records); got != "sess_manual" {
		t.Fatalf("expected manual id, got %q", got)
	}
	if got := resolveSessionChoice("99", records); got != "99" {
		t.Fatalf("expected out-of-range number to pass through, got %q", got)
	}
}

func TestFormatSessionListHandlesEmpty(t *testing.T) {
	if got := formatSessionList(nil); got != "没有可恢复的 session。" {
		t.Fatalf("unexpected empty list text: %q", got)
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
