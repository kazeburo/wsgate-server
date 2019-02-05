package seq

import "sync"

// Seq struct
type Seq struct {
	i  uint64
	mu *sync.RWMutex
}

// New create sequencer
func New() *Seq {
	return &Seq{
		i:  0,
		mu: new(sync.RWMutex),
	}
}

// Next fetch new sequence
func (s *Seq) Next() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.i = s.i + 1
	return s.i
}
