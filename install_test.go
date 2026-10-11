package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWithHooks(t *testing.T) {
	old := `{"model": "opus", "hooks": {
	  "Stop": [{"hooks": [{"type": "command", "command": "/old/place/cchobi hook"}, {"type": "command", "command": "notify-send done && x<y"}]}],
	  "SessionStart": [{"hooks": [{"type": "command", "command": "/old/place/cchobi hook"}]}],
	  "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "audit"}]}]}}`
	out, err := withHooks([]byte(old), `"/new/cchobi" hook`)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Model string
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Command string }
		}
	}
	json.Unmarshal(out, &s)
	count := func(event, command string) (n int) {
		for _, g := range s.Hooks[event] {
			for _, h := range g.Hooks {
				if h.Command == command {
					n++
				}
			}
		}
		return n
	}
	for _, e := range hookEvents {
		if count(e, `"/new/cchobi" hook`) != 1 || count(e, "/old/place/cchobi hook") != 0 {
			t.Errorf("%s: want exactly the new cchobi hook, got %+v", e, s.Hooks[e])
		}
	}
	if s.Model != "opus" || count("Stop", "notify-send done && x<y") != 1 || count("PreToolUse", "audit") != 1 ||
		s.Hooks["PreToolUse"][0].Matcher != "Bash" {
		t.Errorf("lost someone else's settings:\n%s", out)
	}
	if !strings.Contains(string(out), "x<y") {
		t.Errorf("escaped < in a command:\n%s", out)
	}

	out, _ = withHooks(out, "") // uninstall: back to only the others
	if strings.Contains(string(out), "cchobi") || !strings.Contains(string(out), "notify-send") || !strings.Contains(string(out), "audit") {
		t.Errorf("uninstall:\n%s", out)
	}
	if out, _ := withHooks(nil, ""); strings.TrimSpace(string(out)) != "{}" { // no settings.json yet
		t.Errorf("empty: %s", out)
	}
}
