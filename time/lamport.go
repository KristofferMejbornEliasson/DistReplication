package time

import (
	"strconv"
	"sync"
)

type Lamport struct {
	timestamp uint64
	lock      *sync.Mutex
}

func NewLamport() *Lamport {
	return &Lamport{
		timestamp: 0,
		lock:      &sync.Mutex{},
	}
}

// Now returns the current timestamp value.
func (l *Lamport) Now() uint64 {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.timestamp
}

// Increment increases the timestamp value by 1.
func (l *Lamport) Increment() uint64 {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.timestamp++
	return l.timestamp
}

// Update sets this timestamp's value to be equal to the greater of itself and
// the input Lamport time's value.
func (l *Lamport) Update(other *Lamport) uint64 {
	l.lock.Lock()
	defer l.lock.Unlock()

	if l != other && other != nil {
		l.timestamp = max(l.timestamp, other.Now())
	}

	return l.timestamp
}

// UpdateTime sets this timestamp's value to be equal to the greater of itself
// and the input value.
func (l *Lamport) UpdateTime(other uint64) uint64 {
	l.lock.Lock()
	defer l.lock.Unlock()

	l.timestamp = max(l.timestamp, other)
	return l.timestamp
}

func (l *Lamport) String() string {
	return strconv.FormatUint(l.Now(), 10)
}
