package shirei

import (
	"sync"
	"testing"
)

func TestPostRunsFIFOAndAllowsReentry(t *testing.T) {
	FlushPosted()

	var order []string
	Post(func() {
		order = append(order, "first")
		// Nested Post runs on the following frame, not recursively mid-batch.
		Post(func() {
			order = append(order, "nested")
		})
	})
	Post(func() {
		order = append(order, "second")
	})

	out := RunFrameFn(func() {
		order = append(order, "frame1")
		// Calling Post from inside RunFrameFn must not deadlock and runs next frame.
		Post(func() {
			order = append(order, "from-frame1")
		})
	})
	if !out.NextFrameRequested {
		t.Fatalf("expected NextFrameRequested after Post during frame")
	}

	RunFrameFn(func() {
		order = append(order, "frame2")
	})

	want := []string{"first", "second", "frame1", "nested", "from-frame1", "frame2"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q (full: %v)", i, order[i], want[i], order)
		}
	}
}

func TestPostConcurrentSafe(t *testing.T) {
	FlushPosted()

	const workers = 16
	const perWorker = 50
	var count int

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range perWorker {
				Post(func() {
					count++
				})
			}
		}()
	}

	// Pump frames concurrently while goroutines post.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	for {
		RunFrameFn(func() {})
		select {
		case <-done:
			// Drain any callbacks posted after the last frame started.
			RunFrameFn(func() {})
			if count != workers*perWorker {
				t.Fatalf("count = %d, want %d", count, workers*perWorker)
			}
			return
		default:
		}
	}
}
