package time

import (
	"strconv"
	"sync"
)

type Lamport struct {
	timestamp uint64
	lock      *sync.Mutex
}

func (l *Lamport) Now() uint64 {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.timestamp
}

func (l *Lamport) Increment() uint64 {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.timestamp++
	return l.timestamp
}

func (l *Lamport) Update(other *Lamport) uint64 {
	l.lock.Lock()
	defer l.lock.Unlock()

	if l != other && other != nil {
		otherTime := other.Now()
		if l.timestamp < otherTime {
			l.timestamp = otherTime
		}
	}

	return l.timestamp
}

func (l *Lamport) UpdateTime(other uint64) uint64 {
	l.lock.Lock()
	defer l.lock.Unlock()

	if l.timestamp < other {
		l.timestamp = other
	}

	return l.timestamp
}

func (l *Lamport) String() string {
	l.lock.Lock()
	defer l.lock.Unlock()
	return strconv.FormatUint(l.timestamp, 10)
}
