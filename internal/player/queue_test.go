package player

import (
	"testing"
	"time"

	"github.com/user/dc-music-bot/internal/audio"
)

func TestQueueOperations(t *testing.T) {
	q := NewQueue()
	if !q.IsEmpty() {
		t.Fatal("expected new queue to be empty")
	}

	t1 := &audio.Track{ID: "1", Title: "Song 1", Duration: 3 * time.Minute}
	t2 := &audio.Track{ID: "2", Title: "Song 2", Duration: 4 * time.Minute}
	t3 := &audio.Track{ID: "3", Title: "Song 3", Duration: 2 * time.Minute}

	q.Push(t1)
	q.Push(t2)
	q.PushFront(t3)

	if q.Size() != 3 {
		t.Fatalf("expected queue size 3, got %d", q.Size())
	}

	popped := q.Pop()
	if popped.ID != "3" {
		t.Fatalf("expected front track '3', got '%s'", popped.ID)
	}

	removed, err := q.Remove(1)
	if err != nil {
		t.Fatalf("unexpected remove error: %v", err)
	}
	if removed.ID != "2" {
		t.Fatalf("expected removed track '2', got '%s'", removed.ID)
	}

	if q.Size() != 1 {
		t.Fatalf("expected size 1, got %d", q.Size())
	}

	q.Clear()
	if !q.IsEmpty() {
		t.Fatal("expected cleared queue to be empty")
	}
}

func TestQueueLoopMode(t *testing.T) {
	q := NewQueue()
	if q.LoopMode() != LoopOff {
		t.Errorf("expected default LoopOff, got %v", q.LoopMode())
	}

	q.SetLoopMode(LoopTrack)
	if q.LoopMode() != LoopTrack || q.LoopMode().String() != "Track" {
		t.Errorf("expected LoopTrack, got %v", q.LoopMode())
	}

	q.SetLoopMode(LoopQueue)
	if q.LoopMode() != LoopQueue || q.LoopMode().String() != "Queue" {
		t.Errorf("expected LoopQueue, got %v", q.LoopMode())
	}
}
