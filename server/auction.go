package main

import (
	"fmt"
	"sync"
	"time"
)

type Auction struct {
	start      *time.Time // Point in time when auction starts
	end        *time.Time // Point in time when auction ends
	leadingBid *uint64    // Leading bid amount.
	leadingID  *int64     // ID of leading bidder. Nil if no-one has bid
	lock       *sync.Mutex
}

// Reconstruct creates an Auction based on unix start- and end-timestamps.
func Reconstruct(leadingBid *uint64, leadingID *int64, startUnix int64, endUnix int64) *Auction {
	start := time.Unix(startUnix, 0)
	end := time.Unix(endUnix, 0)
	return &Auction{
		start:      &start,
		end:        &end,
		leadingBid: leadingBid,
		leadingID:  leadingID,
	}
}

// StartNewAuction creates a new Auction which starts now, and runs for 100 seconds.
func StartNewAuction(startingBid uint64) *Auction {
	return NewAuctionPeriod(startingBid, time.Now(), 100*time.Second)
}

// StartNewAuctionPeriod creates a new Auction which starts now, and runs for
// the given time.Duration.
func StartNewAuctionPeriod(startingBid uint64, duration time.Duration) *Auction {
	return NewAuctionPeriod(startingBid, time.Now(), duration)
}

// NewAuction creates a new Auction to run between the given start and end times.
func NewAuction(startingBid uint64, start time.Time, end time.Time) *Auction {
	fmt.Printf("Creating new auction in period between %v and %v.\n", start, end)
	return &Auction{
		start:      &start,
		end:        &end,
		leadingBid: &startingBid,
		leadingID:  nil,
		lock:       &sync.Mutex{},
	}
}

// NewAuctionPeriod creates a new Auction to start at the given start time and
// run for the given duration.
func NewAuctionPeriod(startingBid uint64, start time.Time, duration time.Duration) *Auction {
	return NewAuction(startingBid, start, start.Add(duration))
}

func (a *Auction) IsActive() bool {
	a.lock.Lock()
	defer a.lock.Unlock()
	return a.isActive()
}

func (a *Auction) isActive() bool {
	now := time.Now()
	return now.After(*a.start) && now.Before(*a.end)
}

// TryBid attempts to update the leading bid with the given amount.
// If the auction is currently active and the given amount greater than
// the current leading bid, then the auction is updated.
//
// Returns true if the auction was updated as a result of this function.
//
// A person can bid even if they are already the leading bidder.
func (a *Auction) TryBid(bidderID int64, amount uint64) bool {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.isActive() && (a.leadingBid == nil || *a.leadingBid < amount) {
		a.leadingBid = &amount
		a.leadingID = &bidderID
		return true
	}
	return false
}
