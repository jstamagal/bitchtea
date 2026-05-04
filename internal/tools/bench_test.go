package tools

import (
	"context"
	"testing"
)

// BenchmarkExecBash measures the startup/execution latency of a trivial bash
// command through execBash, which exercises the exec.CommandContext path.
func BenchmarkExecBash(b *testing.B) {
	reg := NewRegistry(b.TempDir(), b.TempDir())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := reg.Execute(context.Background(), "bash", `{"command":"echo hi"}`)
		if err != nil {
			b.Fatal(err)
		}
		if out != "hi\n" {
			b.Fatalf("unexpected output: %q", out)
		}
	}
}
