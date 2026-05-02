package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	"charm.land/fantasy"
)

// --- safeSend ---------------------------------------------------------------
//
// safeSend(ctx, ch, ev) blocks on `ch <- ev` until either the receiver picks
// up the value (returns nil) or ctx is cancelled (returns ctx.Err()). These
// tests pin both branches plus the buffered-channel and cancel-while-blocked
// behaviors. They use a 2-second hard timeout per case to ensure the test
// suite never hangs if safeSend is rewritten in a way that loses the
// cancellation guarantee.

const safeSendTestTimeout = 2 * time.Second

func TestSafeSend_NormalSendUnbuffered(t *testing.T) {
	ch := make(chan StreamEvent)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- safeSend(ctx, ch, StreamEvent{Type: "text", Text: "hi"})
	}()

	select {
	case got := <-ch:
		if got.Type != "text" || got.Text != "hi" {
			t.Fatalf("received wrong event: %+v", got)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("never received event from safeSend on unbuffered channel")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("safeSend returned %v on successful send, want nil", err)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("safeSend did not return after value was received")
	}
}

func TestSafeSend_BufferedChannelDoesNotBlock(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- safeSend(ctx, ch, StreamEvent{Type: "thinking"})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("safeSend returned %v on buffered send, want nil", err)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("safeSend did not return immediately on buffered channel with capacity")
	}

	select {
	case got := <-ch:
		if got.Type != "thinking" {
			t.Fatalf("buffered value wrong: %+v", got)
		}
	default:
		t.Fatal("expected value to be sitting in the buffered channel")
	}
}

func TestSafeSend_ContextAlreadyCancelled(t *testing.T) {
	// When ctx is already cancelled and the channel cannot accept the value
	// (no receiver, no buffer), safeSend must return ctx.Err() and the value
	// must NOT land on the channel.
	ch := make(chan StreamEvent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- safeSend(ctx, ch, StreamEvent{Type: "text", Text: "should not land"})
	}()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("safeSend returned %v, want context.Canceled", err)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("safeSend did not return after ctx was already cancelled")
	}

	// Drain attempt — the value must not be sitting on the channel.
	select {
	case got := <-ch:
		t.Fatalf("safeSend leaked event onto channel after cancel: %+v", got)
	default:
	}
}

func TestSafeSend_ContextCancelledWhileBlocked(t *testing.T) {
	// safeSend starts blocked (unbuffered channel, no receiver). Then we
	// cancel the context. safeSend must unblock and return ctx.Err() rather
	// than wait forever for a receiver.
	ch := make(chan StreamEvent)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- safeSend(ctx, ch, StreamEvent{Type: "text", Text: "blocked"})
	}()

	// Give the goroutine a moment to enter the select. Without a small
	// scheduling beat the cancel can race ahead of the select{} in safeSend
	// and the test still passes — but the explicit beat documents intent.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("safeSend returned %v, want context.Canceled", err)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("safeSend did not unblock on ctx cancel")
	}
}

// --- toolResultText ---------------------------------------------------------
//
// toolResultText extracts the text payload from a fantasy.ToolResultOutputContent.
// Today it handles two shapes — the value ToolResultOutputContentText and a
// pointer *ToolResultOutputContentText — and returns "" for everything else
// (including a nil pointer of the recognized pointer type, and any unknown
// concrete type that satisfies the interface). These tests pin that contract.

func TestToolResultText_ValueTextPart(t *testing.T) {
	got := toolResultText(fantasy.ToolResultOutputContentText{Text: "hello"})
	if got != "hello" {
		t.Fatalf("toolResultText(value) = %q, want %q", got, "hello")
	}
}

func TestToolResultText_PointerTextPart(t *testing.T) {
	got := toolResultText(&fantasy.ToolResultOutputContentText{Text: "ptr-hello"})
	if got != "ptr-hello" {
		t.Fatalf("toolResultText(pointer) = %q, want %q", got, "ptr-hello")
	}
}

func TestToolResultText_NilPointerReturnsEmpty(t *testing.T) {
	// A typed-nil *ToolResultOutputContentText must not panic; current code
	// returns "" via the `if v != nil` guard.
	var nilPtr *fantasy.ToolResultOutputContentText
	got := toolResultText(nilPtr)
	if got != "" {
		t.Fatalf("toolResultText(nil pointer) = %q, want empty string", got)
	}
}

func TestToolResultText_EmptyValueText(t *testing.T) {
	// A value-type with an empty Text field returns "" — not because of an
	// "unknown" fallthrough but because v.Text is the empty string. Pinning
	// this so a future refactor doesn't accidentally substitute a placeholder.
	got := toolResultText(fantasy.ToolResultOutputContentText{Text: ""})
	if got != "" {
		t.Fatalf("toolResultText(empty value) = %q, want empty string", got)
	}
}

func TestToolResultText_UnknownErrorTypeReturnsEmpty(t *testing.T) {
	// fantasy.ToolResultOutputContentError satisfies ToolResultOutputContent
	// but is not handled by toolResultText today. Behavior: returns "".
	got := toolResultText(fantasy.ToolResultOutputContentError{Error: errors.New("boom")})
	if got != "" {
		t.Fatalf("toolResultText(error type) = %q, want empty string", got)
	}
}

func TestToolResultText_UnknownMediaTypeReturnsEmpty(t *testing.T) {
	// fantasy.ToolResultOutputContentMedia is the other built-in alternative
	// shape. Today toolResultText does not extract anything from it.
	got := toolResultText(fantasy.ToolResultOutputContentMedia{
		Data:      "ZGF0YQ==",
		MediaType: "image/png",
	})
	if got != "" {
		t.Fatalf("toolResultText(media type) = %q, want empty string", got)
	}
}

func TestToolResultText_NilInterfaceReturnsEmpty(t *testing.T) {
	// A bare nil interface flows through the type switch with no match and
	// hits the trailing `return ""`. Pinning this so the function stays
	// crash-free at the agent boundary if a provider ever sends a tool
	// result with no Result payload.
	got := toolResultText(nil)
	if got != "" {
		t.Fatalf("toolResultText(nil interface) = %q, want empty string", got)
	}
}
