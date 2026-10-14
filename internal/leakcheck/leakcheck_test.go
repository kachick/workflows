package leakcheck

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

type mockChecker struct {
	checkFunc func(ctx context.Context, input string) error
}

func (m *mockChecker) Check(ctx context.Context, input string) error {
	return m.checkFunc(ctx, input)
}

func TestCheckMultiple(t *testing.T) {
	ctx := context.Background()

	t.Run("passes all clean inputs", func(t *testing.T) {
		called := 0
		mock := &mockChecker{
			checkFunc: func(ctx context.Context, input string) error {
				called++
				return nil
			},
		}

		err := CheckMultiple(ctx, mock, "clean title", "clean body", "clean diff")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if called != 3 {
			t.Fatalf("expected 3 calls, got %d", called)
		}
	})

	t.Run("stops on first leak error", func(t *testing.T) {
		called := 0
		mock := &mockChecker{
			checkFunc: func(ctx context.Context, input string) error {
				called++
				if input == "leaky" {
					return ErrLeakDetected
				}
				return nil
			},
		}

		err := CheckMultiple(ctx, mock, "clean", "leaky", "never reached")
		if !errors.Is(err, ErrLeakDetected) {
			t.Fatalf("expected ErrLeakDetected, got %v", err)
		}
		if called != 2 {
			t.Fatalf("expected 2 calls, got %d", called)
		}
	})
}

func TestBetterleaksChecker(t *testing.T) {
	if _, err := exec.LookPath("betterleaks"); err != nil {
		t.Skip("betterleaks executable not found in PATH")
	}

	ctx := context.Background()
	checker := &BetterleaksChecker{}

	t.Run("clean text passes", func(t *testing.T) {
		err := checker.Check(ctx, "Bump flake.lock and related dependencies")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("detects secret pattern", func(t *testing.T) {
		// Use dummy GitHub PAT pattern documented in upstream docs
		dummyToken := "GITHUB_TOKEN=ghp_aB3dE5fG7hI9jK1mN3pQ5rS7tU9vW1xY3zA5"
		err := checker.Check(ctx, dummyToken)
		if err == nil {
			t.Fatal("expected error for detected secret, got nil")
		}
		if !errors.Is(err, ErrLeakDetected) {
			t.Fatalf("expected ErrLeakDetected, got: %v", err)
		}
	})
}
