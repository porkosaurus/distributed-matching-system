package main

import (
	"fmt"
	"sync"
)

// RiskLimits defines the limits for a participant
type RiskLimits struct {
	MaxPositionSize uint64  // max shares held long or short
	MaxNotional     uint64  // max value of a single order in cents
	MaxOrderSize    uint64  // max quantity per single order
	PriceCollarPct  float64 // max % away from last price (e.g. 0.05 = 5%)
}

// Position tracks a participant's current position
type Position struct {
	NetPosition int64  // positive = long, negative = short
	TotalBought uint64
	TotalSold   uint64
}

// RiskManager checks orders against limits before sequencing
type RiskManager struct {
	mu        sync.RWMutex
	positions map[uint64]*Position // keyed by participant ID
	limits    RiskLimits
	lastPrice uint64 // last trade price for collar check
}

func NewRiskManager(limits RiskLimits) *RiskManager {
	return &RiskManager{
		positions: make(map[uint64]*Position),
		limits:    limits,
		lastPrice: 10000, // default starting price in cents
	}
}

// CheckOrder returns nil if the order passes all risk checks
// or an error describing which check failed
func (r *RiskManager) CheckOrder(order Order) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. order size check
	if order.Quantity > r.limits.MaxOrderSize {
		return fmt.Errorf("order size %d exceeds max %d",
			order.Quantity, r.limits.MaxOrderSize)
	}

	// 2. notional check — price * quantity can't exceed limit
	notional := order.Price * order.Quantity
	if notional > r.limits.MaxNotional {
		return fmt.Errorf("notional %d exceeds max %d",
			notional, r.limits.MaxNotional)
	}

	// 3. price collar — price can't be more than X% from last trade
	if r.lastPrice > 0 {
		var pctMove float64
		if order.Price > r.lastPrice {
			pctMove = float64(order.Price-r.lastPrice) / float64(r.lastPrice)
		} else {
			pctMove = float64(r.lastPrice-order.Price) / float64(r.lastPrice)
		}
		if pctMove > r.limits.PriceCollarPct {
			return fmt.Errorf("price %d is %.1f%% from last price %d, exceeds collar of %.1f%%",
				order.Price, pctMove*100, r.lastPrice, r.limits.PriceCollarPct*100)
		}
	}

	// 4. position limit check
	pos := r.getPosition(order.ID)
	if order.Side == "buy" {
		newPos := pos.NetPosition + int64(order.Quantity)
		if newPos > int64(r.limits.MaxPositionSize) {
			return fmt.Errorf("buy would exceed max position %d",
				r.limits.MaxPositionSize)
		}
	} else {
		newPos := pos.NetPosition - int64(order.Quantity)
		if newPos < -int64(r.limits.MaxPositionSize) {
			return fmt.Errorf("sell would exceed max short position %d",
				r.limits.MaxPositionSize)
		}
	}

	return nil
}

// RecordFill updates positions after a fill occurs
func (r *RiskManager) RecordFill(fill Fill) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// update buyer position
	buyPos := r.getOrCreatePosition(fill.BuyOrderID)
	buyPos.NetPosition += int64(fill.Quantity)
	buyPos.TotalBought += fill.Quantity

	// update seller position
	sellPos := r.getOrCreatePosition(fill.SellOrderID)
	sellPos.NetPosition -= int64(fill.Quantity)
	sellPos.TotalSold += fill.Quantity

	// update last trade price for collar checks
	r.lastPrice = fill.Price
}

// getPosition returns position for a participant, or empty if none exists
// caller must hold at least RLock
func (r *RiskManager) getPosition(participantID uint64) Position {
	if pos, ok := r.positions[participantID]; ok {
		return *pos
	}
	return Position{}
}

// getOrCreatePosition returns or creates a position
// caller must hold Lock
func (r *RiskManager) getOrCreatePosition(participantID uint64) *Position {
	if _, ok := r.positions[participantID]; !ok {
		r.positions[participantID] = &Position{}
	}
	return r.positions[participantID]
}