package ui

import (
	"fmt"
	"strings"
	"testing"

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
