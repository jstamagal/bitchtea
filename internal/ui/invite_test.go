package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/session"
)

func TestInviteCommandJoinsPersonaToCurrentChannel(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleInviteCommand(m, "/invite reviewer", []string{"/invite", "reviewer"})
	if !result.membership.IsJoined("main", "reviewer") {
		t.Error("expected reviewer to be joined in #main")
	}
}

func TestInviteCommandJoinsPersonaToNamedChannel(t *testing.T) {
	m := newTestModel(t)
	m.focus.SetFocus(Channel("ops"))
	result, _ := handleInviteCommand(m, "/invite oncall #main", []string{"/invite", "oncall", "#main"})
	if !result.membership.IsJoined("main", "oncall") {
		t.Error("expected oncall to be joined in #main")
	}
	if result.membership.IsJoined("ops", "oncall") {
		t.Error("oncall should NOT be in #ops")
	}
}

func TestInviteCommandMissingArgErrors(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleInviteCommand(m, "/invite", []string{"/invite"})
	msg := result.messages[len(result.messages)-1]
	if msg.Type != MsgError {
		t.Fatalf("expected error, got %v: %q", msg.Type, msg.Content)
	}
}

func TestInviteCommandRefusesDMContext(t *testing.T) {
	m := newTestModel(t)
	m.focus.SetFocus(Direct("buddy"))
	result, _ := handleInviteCommand(m, "/invite reviewer", []string{"/invite", "reviewer"})
	msg := result.messages[len(result.messages)-1]
	if msg.Type != MsgError {
		t.Fatalf("expected error for DM context, got %v: %q", msg.Type, msg.Content)
	}
}

func TestInviteCommandIdempotentShowsAlreadyIn(t *testing.T) {
	m := newTestModel(t)
	m2, _ := handleInviteCommand(m, "/invite reviewer", []string{"/invite", "reviewer"})
	result, _ := handleInviteCommand(m2, "/invite reviewer", []string{"/invite", "reviewer"})
	msg := result.messages[len(result.messages)-1]
	if msg.Type == MsgError {
		t.Fatalf("unexpected error on second invite: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "already in") {
		t.Errorf("expected 'already in' message, got %q", msg.Content)
	}
}

func TestInviteCommandShowsJoinNoticeAndCatchup(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleInviteCommand(m, "/invite reviewer", []string{"/invite", "reviewer"})
	if len(result.messages) < 2 {
		t.Fatalf("expected at least 2 messages after invite, got %d", len(result.messages))
	}
	joinMsg := result.messages[len(result.messages)-2]
	if !strings.Contains(joinMsg.Content, "reviewer joined #main") {
		t.Errorf("expected join notice, got %q", joinMsg.Content)
	}
	catchupMsg := result.messages[len(result.messages)-1]
	if catchupMsg.Type != MsgSystem {
		t.Errorf("expected system message for catch-up, got %v", catchupMsg.Type)
	}
}

// TestInviteCommandInjectsPersonaIntoAgentContext is the bt-wire.4 acceptance
// test: after /invite reviewer in #main, the agent's per-context history for
// #main must contain a note that names the persona and the channel, so the
// next streamed turn sees the membership change. Without this injection, the
// LLM has no way to learn about /invite — membership is pure UI metadata.
func TestInviteCommandInjectsPersonaIntoAgentContext(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleInviteCommand(m, "/invite reviewer", []string{"/invite", "reviewer"})

	rm := result
	rm.agent.SetContext("#main")
	msgs := rm.agent.Messages()

	if len(msgs) <= rm.agent.BootstrapMessageCount() {
		t.Fatalf("expected agent #main context to gain messages beyond bootstrap, got %d (bootstrap=%d)",
			len(msgs), rm.agent.BootstrapMessageCount())
	}

	combined := joinMessageText(msgs[rm.agent.BootstrapMessageCount():])
	for _, want := range []string{"reviewer", "#main", "joined", "membership update"} {
		if !strings.Contains(combined, want) {
			t.Errorf("agent #main context missing %q in injected note; transcript:\n%s", want, combined)
		}
	}
}

// TestInviteCommandInjectsIntoNamedChannelNotFocus verifies cross-context
// routing: when /invite targets a different channel than the active focus,
// the persona note must land in the target channel's per-context history,
// not the active focus's. This protects #engineering's history from leaking
// into #ops just because the user typed /invite while focused on #ops.
func TestInviteCommandInjectsIntoNamedChannelNotFocus(t *testing.T) {
	m := newTestModel(t)
	m.focus.SetFocus(Channel("ops"))
	result, _ := handleInviteCommand(m, "/invite oncall #engineering",
		[]string{"/invite", "oncall", "#engineering"})

	rm := result
	rm.agent.SetContext("#engineering")
	engMsgs := rm.agent.Messages()
	engBoot := rm.agent.BootstrapMessageCount()
	engInjected := joinMessageText(engMsgs[engBoot:])
	if !strings.Contains(engInjected, "oncall") || !strings.Contains(engInjected, "#engineering") {
		t.Errorf("expected #engineering context to gain persona note, got: %q", engInjected)
	}

	rm.agent.SetContext("#ops")
	opsMsgs := rm.agent.Messages()
	opsBoot := rm.agent.BootstrapMessageCount()
	opsInjected := joinMessageText(opsMsgs[opsBoot:])
	if strings.Contains(opsInjected, "oncall") {
		t.Errorf("#ops context should not see #engineering invite note, got: %q", opsInjected)
	}
}

// TestKickCommandInjectsRemovalIntoAgentContext mirrors the invite path:
// after /kick, the agent's channel history must learn the persona has left
// so it stops modeling them as a participant.
func TestKickCommandInjectsRemovalIntoAgentContext(t *testing.T) {
	m := newTestModel(t)
	m.membership.Invite("main", "reviewer")
	// Pre-seed the join note so the kick note doesn't accidentally pass on
	// "reviewer" alone — the kick text must mention removal explicitly.
	m.agent.InitContext("#main")
	m.agent.InjectNoteInContext("#main", "reviewer joined seed")

	result, _ := handleKickCommand(m, "/kick reviewer", []string{"/kick", "reviewer"})

	rm := result
	rm.agent.SetContext("#main")
	msgs := rm.agent.Messages()
	combined := joinMessageText(msgs[rm.agent.BootstrapMessageCount():])

	for _, want := range []string{"reviewer", "removed", "#main", "membership update"} {
		if !strings.Contains(combined, want) {
			t.Errorf("agent #main context missing %q in kick note; transcript:\n%s", want, combined)
		}
	}
}

// joinMessageText is a test helper that flattens a slice of fantasy.Message
// into a single searchable string by concatenating each message's text parts.
// Mirrors agent.messageText (unexported) for the ui package.
func joinMessageText(msgs []fantasy.Message) string {
	var sb strings.Builder
	for _, msg := range msgs {
		for _, part := range msg.Content {
			switch p := part.(type) {
			case fantasy.TextPart:
				sb.WriteString(p.Text)
				sb.WriteString("\n")
			case *fantasy.TextPart:
				if p != nil {
					sb.WriteString(p.Text)
					sb.WriteString("\n")
				}
			}
		}
	}
	return sb.String()
}

func TestInviteCommandPersistsMembership(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleInviteCommand(m, "/invite reviewer", []string{"/invite", "reviewer"})
	restored := LoadMembershipManager(result.config.SessionDir)
	if !restored.IsJoined("main", "reviewer") {
		t.Error("membership not persisted after /invite")
	}
}

func TestKickCommandRemovesPersona(t *testing.T) {
	m := newTestModel(t)
	m.membership.Invite("main", "reviewer")
	result, _ := handleKickCommand(m, "/kick reviewer", []string{"/kick", "reviewer"})
	if result.membership.IsJoined("main", "reviewer") {
		t.Error("reviewer should be gone after /kick")
	}
}

func TestKickCommandMissingArgErrors(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleKickCommand(m, "/kick", []string{"/kick"})
	msg := result.messages[len(result.messages)-1]
	if msg.Type != MsgError {
		t.Fatalf("expected error, got %v: %q", msg.Type, msg.Content)
	}
}

func TestKickCommandNotPresentErrors(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleKickCommand(m, "/kick ghost", []string{"/kick", "ghost"})
	msg := result.messages[len(result.messages)-1]
	if msg.Type != MsgError {
		t.Fatalf("expected error for non-member kick, got %v: %q", msg.Type, msg.Content)
	}
	if !strings.Contains(msg.Content, "not in") {
		t.Errorf("expected 'not in' message, got %q", msg.Content)
	}
}

func TestKickCommandPersistsMembership(t *testing.T) {
	m := newTestModel(t)
	m.membership.Invite("main", "reviewer")
	result, _ := handleKickCommand(m, "/kick reviewer", []string{"/kick", "reviewer"})
	restored := LoadMembershipManager(result.config.SessionDir)
	if restored.IsJoined("main", "reviewer") {
		t.Error("membership should be cleared after /kick")
	}
}

func TestChannelsCommandShowsMembers(t *testing.T) {
	m := newTestModel(t)
	m.membership.Invite("main", "debugger")
	m.membership.Invite("main", "reviewer")
	result, _ := handleChannelsCommand(m, "/channels", []string{"/channels"})
	content := result.messages[len(result.messages)-1].Content
	if !strings.Contains(content, "reviewer") || !strings.Contains(content, "debugger") {
		t.Errorf("channels output missing members: %q", content)
	}
}

func TestBuildChannelCatchup_noSession(t *testing.T) {
	catchup := buildChannelCatchup(nil, "#main", 50)
	if !strings.Contains(catchup, "no session history") {
		t.Errorf("expected no-session message, got %q", catchup)
	}
}

func TestBuildChannelCatchup_noPriorEntries(t *testing.T) {
	dir := t.TempDir()
	sess, _ := session.New(dir)
	catchup := buildChannelCatchup(sess, "#main", 50)
	if !strings.Contains(catchup, "no prior conversation") {
		t.Errorf("expected no-prior message, got %q", catchup)
	}
}

func TestBuildChannelCatchup_returnsLastN(t *testing.T) {
	dir := t.TempDir()
	sess, _ := session.New(dir)
	for i := 0; i < 10; i++ {
		_ = sess.Append(session.Entry{
			Role:    "user",
			Content: fmt.Sprintf("msg %d", i),
			Context: "#main",
		})
	}
	catchup := buildChannelCatchup(sess, "#main", 3)
	if !strings.Contains(catchup, "3 messages") {
		t.Errorf("expected '3 messages' in catch-up, got: %q", catchup)
	}
	if strings.Contains(catchup, "msg 0") {
		t.Errorf("catch-up should only have last 3, not msg 0: %q", catchup)
	}
	if !strings.Contains(catchup, "msg 9") {
		t.Errorf("catch-up should include most recent msg 9, got: %q", catchup)
	}
}

func TestBuildChannelCatchup_excludesToolEntries(t *testing.T) {
	dir := t.TempDir()
	sess, _ := session.New(dir)
	_ = sess.Append(session.Entry{Role: "user", Content: "hello", Context: "#main"})
	_ = sess.Append(session.Entry{Role: "tool", Content: "tool output", Context: "#main", ToolCallID: "call_1"})
	_ = sess.Append(session.Entry{Role: "assistant", Content: "done", Context: "#main"})

	catchup := buildChannelCatchup(sess, "#main", 50)
	if strings.Contains(catchup, "tool output") {
		t.Error("catch-up should not include tool messages")
	}
	if !strings.Contains(catchup, "hello") {
		t.Error("catch-up should include user message")
	}
	if !strings.Contains(catchup, "done") {
		t.Error("catch-up should include assistant message")
	}
}

func TestBuildChannelCatchup_excludesOtherChannels(t *testing.T) {
	dir := t.TempDir()
	sess, _ := session.New(dir)
	_ = sess.Append(session.Entry{Role: "user", Content: "main msg", Context: "#main"})
	_ = sess.Append(session.Entry{Role: "user", Content: "ops msg", Context: "#ops"})

	catchup := buildChannelCatchup(sess, "#main", 50)
	if strings.Contains(catchup, "ops msg") {
		t.Error("catch-up should only include #main entries")
	}
	if !strings.Contains(catchup, "main msg") {
		t.Error("catch-up should include #main entry")
	}
}
