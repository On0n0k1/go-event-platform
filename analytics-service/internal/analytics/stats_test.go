package analytics

import (
	"sync"
	"testing"
)

func TestStatsRecordAndSnapshot(t *testing.T) {
	s := &Stats{}

	want := Snapshot{}
	if got := s.Snapshot(); got != want {
		t.Fatalf("initial snapshot = %+v, want %+v", got, want)
	}

	s.Record(5)
	s.Record(3)

	want = Snapshot{OrdersCount: 2, TotalQuantityReserved: 8}
	if got := s.Snapshot(); got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
}

func TestStatsRecordIsConcurrencySafe(t *testing.T) {
	s := &Stats{}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Record(1)
		}()
	}
	wg.Wait()

	want := Snapshot{OrdersCount: 100, TotalQuantityReserved: 100}
	if got := s.Snapshot(); got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
}
