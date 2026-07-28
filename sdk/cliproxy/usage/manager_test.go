package usage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerateEnabledDefaultsNilToTrue(t *testing.T) {
	if !GenerateEnabled(nil) {
		t.Fatalf("GenerateEnabled(nil) = false, want true")
	}
}

func TestGenerateEnabledHonorsExplicitFalse(t *testing.T) {
	if GenerateEnabled(GenerateFlag(false)) {
		t.Fatalf("GenerateEnabled(false) = true, want false")
	}
}

func TestGenerateEnabledHonorsExplicitTrue(t *testing.T) {
	if !GenerateEnabled(GenerateFlag(true)) {
		t.Fatalf("GenerateEnabled(true) = false, want true")
	}
}

func TestGenerateFromContextDefaultsMissingToTrue(t *testing.T) {
	if !GenerateFromContext(context.Background()) {
		t.Fatalf("GenerateFromContext(background) = false, want true")
	}
}

func TestGenerateFromContextHonorsExplicitFalse(t *testing.T) {
	ctx := WithGenerate(context.Background(), false)
	if GenerateFromContext(ctx) {
		t.Fatalf("GenerateFromContext(false) = true, want false")
	}
}

func TestRecordOmittedGenerateIsEnabled(t *testing.T) {
	// Existing callers construct Record without setting Generate.
	// Omission must remain distinguishable from explicit false and default to true.
	record := Record{
		Provider: "openai",
		Model:    "gpt-5.4",
	}
	if record.Generate != nil {
		t.Fatalf("Record.Generate = %v, want nil for omitted field", record.Generate)
	}
	if !GenerateEnabled(record.Generate) {
		t.Fatalf("GenerateEnabled(omitted) = false, want true")
	}
}

type blockingUsagePlugin struct {
	started chan struct{}
	release chan struct{}
	count   atomic.Int64
}

func (p *blockingUsagePlugin) HandleUsage(context.Context, Record) {
	if p.count.Add(1) == 1 {
		close(p.started)
		<-p.release
	}
}

func TestManagerStopAndWaitDrainsQueue(t *testing.T) {
	manager := NewManager(2)
	plugin := &blockingUsagePlugin{started: make(chan struct{}), release: make(chan struct{})}
	manager.Register(plugin)
	manager.Start(context.Background())
	manager.Publish(context.Background(), Record{Model: "first"})
	manager.Publish(context.Background(), Record{Model: "second"})
	<-plugin.started

	done := make(chan error, 1)
	go func() { done <- manager.StopAndWait(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("StopAndWait returned before queue drained: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(plugin.release)
	if err := <-done; err != nil {
		t.Fatalf("StopAndWait() error = %v", err)
	}
	if got := plugin.count.Load(); got != 2 {
		t.Fatalf("delivered records = %d, want 2", got)
	}
}
