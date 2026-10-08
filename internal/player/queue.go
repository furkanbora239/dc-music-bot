package player

import (
	"math/rand"
	"sync"
	"time"

	"github.com/user/dc-music-bot/internal/audio"
)

// LoopMode defines the looping behavior.
type LoopMode int

const (
	LoopOff LoopMode = iota
	LoopTrack
	LoopQueue
)

func (m LoopMode) String() string {
	switch m {
	case LoopTrack:
		return "Track"
	case LoopQueue:
		return "Queue"
	default:
		return "Off"
	}
}

// Queue provides a thread-safe playlist queue for tracks.
type Queue struct {
	mu       sync.RWMutex
	tracks   []*audio.Track
	loopMode LoopMode
	rnd      *rand.Rand
}

// NewQueue creates an initialized Queue.
func NewQueue() *Queue {
	return &Queue{
		tracks:   make([]*audio.Track, 0),
		loopMode: LoopOff,
		rnd:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Push adds a track to the end of the queue.
func (q *Queue) Push(track *audio.Track) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tracks = append(q.tracks, track)
}

// Pop removes and returns the first track in the queue, or nil if empty.
func (q *Queue) Pop() *audio.Track {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tracks) == 0 {
		return nil
	}
	track := q.tracks[0]
	q.tracks = q.tracks[1:]
	return track
}

// Clear empties the queue.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tracks = make([]*audio.Track, 0)
}

// List returns a copy of current tracks in the queue.
func (q *Queue) List() []*audio.Track {
	q.mu.RLock()
	defer q.mu.RUnlock()
	res := make([]*audio.Track, len(q.tracks))
	copy(res, q.tracks)
	return res
}

// Size returns the count of tracks currently in queue.
func (q *Queue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.tracks)
}

// IsEmpty returns true if queue has no tracks.
func (q *Queue) IsEmpty() bool {
	return q.Size() == 0
}

// Shuffle randomly reorders the queue.
func (q *Queue) Shuffle() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tracks) <= 1 {
		return
	}
	q.rnd.Shuffle(len(q.tracks), func(i, j int) {
		q.tracks[i], q.tracks[j] = q.tracks[j], q.tracks[i]
	})
}

// SetLoopMode sets repeat mode.
func (q *Queue) SetLoopMode(mode LoopMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.loopMode = mode
}

// LoopMode returns current repeat mode.
func (q *Queue) LoopMode() LoopMode {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.loopMode
}
