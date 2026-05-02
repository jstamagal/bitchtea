package llm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestToolContextManager_CancelTool(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	defer turnCancel()

	mgr := NewToolContextManager(turnCtx)

	// Create two tool contexts.
	ctx1, cleanup1 := mgr.NewToolContext("tool-1")
	defer cleanup1()
	ctx2, cleanup2 := mgr.NewToolContext("tool-2")
	defer cleanup2()

	// Both should be active.
	if ids := mgr.ActiveToolIDs(); len(ids) != 2 {
		t.Fatalf("expected 2 active tools, got %d", len(ids))
	}

	// Cancel tool-1 only.
	if err := mgr.CancelTool("tool-1"); err != nil {
		t.Fatalf("CancelTool(tool-1): %v", err)
	}

	// tool-1 context should be done.
	select {
	case <-ctx1.Done():
		// ok
	case <-time.After(time.Second):
		t.Fatal("tool-1 context not cancelled after CancelTool")
	}

	// tool-2 context should still be alive.
	select {
	case <-ctx2.Done():
		t.Fatal("tool-2 context cancelled unexpectedly")
	default:
		// ok
	}
}

func TestToolContextManager_TurnCancelPropagates(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	mgr := NewToolContextManager(turnCtx)

	ctx1, cleanup1 := mgr.NewToolContext("tool-1")
	defer cleanup1()

	// Cancel the turn.
	turnCancel()

	// tool context should be done (inherits from turn).
	select {
	case <-ctx1.Done():
		// ok
	case <-time.After(time.Second):
		t.Fatal("tool-1 context not cancelled after turn cancel")
	}
}

func TestToolContextManager_AlreadyCancelledTurn(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	turnCancel() // cancel immediately

	mgr := NewToolContextManager(turnCtx)

	ctx, cleanup := mgr.NewToolContext("tool-1")
	defer cleanup()

	// Should return the turn context (already done).
	if ctx.Err() == nil {
		t.Fatal("expected already-cancelled context")
	}
}

func TestToolContextManager_CancelAll(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	defer turnCancel()

	mgr := NewToolContextManager(turnCtx)

	ctx1, cleanup1 := mgr.NewToolContext("tool-1")
	defer cleanup1()
	ctx2, cleanup2 := mgr.NewToolContext("tool-2")
	defer cleanup2()

	mgr.CancelAll()

	for _, ctx := range []context.Context{ctx1, ctx2} {
		select {
		case <-ctx.Done():
			// ok
		case <-time.After(time.Second):
			t.Fatal("context not cancelled after CancelAll")
		}
	}

	// Map should be empty after CancelAll.
	if ids := mgr.ActiveToolIDs(); len(ids) != 0 {
		t.Fatalf("expected 0 active tools after CancelAll, got %d", len(ids))
	}
}

func TestToolContextManager_CleanupRemovesEntry(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	defer turnCancel()

	mgr := NewToolContextManager(turnCtx)

	_, cleanup := mgr.NewToolContext("tool-1")
	cleanup()

	if ids := mgr.ActiveToolIDs(); len(ids) != 0 {
		t.Fatalf("expected 0 active tools after cleanup, got %d", len(ids))
	}
}

func TestToolContextManager_CancelNonexistent(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	defer turnCancel()

	mgr := NewToolContextManager(turnCtx)

	if err := mgr.CancelTool("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestToolContextManager_ConcurrentAccess(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	defer turnCancel()

	mgr := NewToolContextManager(turnCtx)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cleanup := mgr.NewToolContext(time.Now().String())
			defer cleanup()
			// Immediately cancel some, let others be cleaned up.
			if id%2 == 0 {
				mgr.CancelTool(time.Now().String()) // may or may not exist — that's fine
			}
			_ = ctx
		}(i)
	}
	wg.Wait()

	// No panic = pass. CancelAll to clean up stragglers.
	mgr.CancelAll()
}

func TestToolContextManager_ConcurrentCancelAndNew(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	defer turnCancel()

	mgr := NewToolContextManager(turnCtx)

	var wg sync.WaitGroup
	// Spawn tools and cancel them concurrently.
	for i := 0; i < 100; i++ {
		wg.Add(2)
		id := fmt.Sprintf("tool-%d", i)
		go func() {
			defer wg.Done()
			ctx, cleanup := mgr.NewToolContext(id)
			defer cleanup()
			// Simulate work: wait until context is cancelled or 10ms.
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Millisecond):
			}
		}()
		go func() {
			defer wg.Done()
			// Small delay to let NewToolContext run first sometimes.
			time.Sleep(time.Microsecond)
			mgr.CancelTool(id) // may or may not exist
		}()
	}
	wg.Wait()
	mgr.CancelAll()
}

func TestToolContextManager_ConcurrentCancelAllAndNew(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	defer turnCancel()

	mgr := NewToolContextManager(turnCtx)

	var wg sync.WaitGroup
	// Goroutines creating tools.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, cleanup := mgr.NewToolContext(time.Now().String())
			defer cleanup()
		}()
	}
	// Goroutines calling CancelAll.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.CancelAll()
		}()
	}
	wg.Wait()
}

func TestToolContextManager_CancelDuringUse(t *testing.T) {
	turnCtx, turnCancel := context.WithCancel(context.Background())
	defer turnCancel()

	mgr := NewToolContextManager(turnCtx)

	// Create a tool context and hold it while cancelling.
	ctx, cleanup := mgr.NewToolContext("long-tool")
	defer cleanup()

	var wg sync.WaitGroup
	// Worker using the context.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()
	// Canceller.
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond)
		mgr.CancelTool("long-tool")
	}()
	wg.Wait()
}
