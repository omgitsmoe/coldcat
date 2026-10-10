package importer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFailedImportCleanupContext(t *testing.T) {
	before := time.Now()
	ctx, cancel := failedImportCleanupContext()
	defer cancel()
	after := time.Now()

	deadline, ok := ctx.Deadline()
	if !ok || deadline.Before(before.Add(5*time.Minute)) ||
		deadline.After(after.Add(5*time.Minute)) {
		t.Fatalf("cleanup deadline %v, want five minutes after [%v, %v]", deadline, before, after)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("new cleanup context already canceled: %v", err)
	}

	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("cleanup context cannot be canceled: %v", ctx.Err())
	}
}
