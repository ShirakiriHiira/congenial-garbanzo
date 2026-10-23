package bot

import (
	"strings"
	"testing"
)

// The three cancel entry points report different outcomes for the same
// state, so each wording is pinned here.
func TestCancelPathsReportTheirOwnOutcome(t *testing.T) {
	t.Run("command without a flow", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		text := f.send(userA, "/cancel")
		if !strings.Contains(text, "当前没有进行中的对话。") {
			t.Fatalf("cancel without a flow: %q", text)
		}
	})

	t.Run("command with a flow", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		f.send(userA, "/add")
		text := f.send(userA, "/cancel")
		if !strings.Contains(text, "已取消当前流程，未写入任何修改。") {
			t.Fatalf("cancel with a flow: %q", text)
		}
		if n := f.count("SELECT count(*) FROM bot_dialogs"); n != 0 {
			t.Fatalf("cancel left a draft: %d", n)
		}
	})

	t.Run("cancel button without a flow", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		f.callback(userA, cb("dialog", "cancel"), nil)
		if text := f.stub.lastText(); !strings.Contains(text, "当前没有进行中的流程") {
			t.Fatalf("cancel button without a flow: %q", text)
		}
		if answer := serviceLastAnswer(t, f); answer != "" {
			t.Fatalf("cancel button answered with a toast: %q", answer)
		}
	})

	t.Run("cancel button with a flow", func(t *testing.T) {
		f := newFixture(t)
		f.start(userA)
		f.send(userA, "/add")
		f.callback(userA, cb("dialog", "cancel"), nil)
		if text := f.stub.lastText(); !strings.Contains(text, "已取消当前流程，未写入任何修改。") {
			t.Fatalf("cancel button with a flow: %q", text)
		}
		if answer := serviceLastAnswer(t, f); answer != "" {
			t.Fatalf("cancel button answered with a toast: %q", answer)
		}
	})
}
