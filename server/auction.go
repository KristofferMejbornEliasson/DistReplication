package main

import "time"

type Auction struct {
	start      *time.Time // Point in time when auction starts
	end        *time.Time // Point in time when auction ends
	leadingBid *uint64    // Leading bid amount.
	leadingId  *int64     // ID of leading bidder. Nil if no-one has bid
}

// StartNewAuction creates a new Auction which starts now, and runs for 100 seconds.
func StartNewAuction(startingBid uint64) *Auction {
	return NewAuctionPeriod(startingBid, time.Now(), 100)
}

// StartNewAuctionPeriod creates a new Auction which starts now, and runs for
// the given duration.
func StartNewAuctionPeriod(startingBid uint64, duration time.Duration) *Auction {
	return NewAuctionPeriod(startingBid, time.Now(), duration)
}

// NewAuction creates a new Auction to run between the given start and end times.
func NewAuction(startingBid uint64, start time.Time, end time.Time) *Auction {
	return &Auction{
		start:      &start,
		end:        &end,
		leadingBid: &startingBid,
		leadingId:  nil,
	}
}

// NewAuctionPeriod creates a new Auction to start at the given start time and
// run for the given duration.
func NewAuctionPeriod(startingBid uint64, start time.Time, duration time.Duration) *Auction {
	return NewAuction(startingBid, start, start.Add(duration))
}

func (a *Auction) IsActive() bool {
	now := time.Now()
	return now.After(*a.start) && now.Before(*a.end)
}
