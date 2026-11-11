package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestWithAccountLock: the account's lock is held by one caller at a time, and leaves
// the other accounts alone; fn's error comes back as it is.
func TestWithAccountLock(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	a, _ := newVehicle(t, s, "token-a", "YV1AAAAAAAAAAAAA1")
	b, _ := newVehicle(t, s, "token-b", "YV1BBBBBBBBBBBBB1")

	held, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- s.WithAccountLock(ctx, a, func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	// Another account is not held back.
	done := make(chan error, 1)
	go func() { done <- s.WithAccountLock(ctx, b, func(context.Context) error { return nil }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("another account waited for the lock")
	}

	// The same account waits until the lock is released.
	entered := make(chan struct{})
	second := make(chan error, 1)
	sentinel := errors.New("fn's own")
	go func() {
		second <- s.WithAccountLock(ctx, a, func(context.Context) error {
			close(entered)
			return sentinel
		})
	}()
	select {
	case <-entered:
		t.Fatal("the account's lock taken twice")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the lock never released")
	}
	if err := <-second; !errors.Is(err, sentinel) {
		t.Errorf("error %v, want fn's own", err)
	}
}
