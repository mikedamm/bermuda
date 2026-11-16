package cache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoCoalesces(t *testing.T) {
	c := New(10)
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, _ := Do(c, "k", func() (int, time.Duration, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond)
				return 42, time.Minute, nil
			})
			if v != 42 {
				t.Errorf("got %d", v)
			}
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("loader ran %d times, want 1", n)
	}
}

func TestEvictsOldest(t *testing.T) {
	c := New(2)
	for _, k := range []string{"a", "b", "c"} {
		Do(c, k, func() (string, time.Duration, error) { return k, time.Minute, nil })
	}
	ran := false
	Do(c, "a", func() (string, time.Duration, error) { ran = true; return "a", time.Minute, nil })
	if !ran {
		t.Fatal("oldest entry was not evicted")
	}
}
