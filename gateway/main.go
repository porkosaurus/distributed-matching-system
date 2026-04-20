package main

import (
	"log"
)

func main() {
	orderCh := make(chan Order, 1000)
	seqCh   := make(chan SequencedOrder, 1000)
	fillCh  := make(chan Fill, 1000)

	// configure risk limits
	// these are intentionally generous for development
	// tighten them to test rejection behavior
	risk := NewRiskManager(RiskLimits{
		MaxPositionSize: 100000,  // max 100k shares net position
		MaxNotional:     50000000, // max $500,000 per order (in cents)
		MaxOrderSize:    10000,   // max 10k shares per order
		PriceCollarPct:  0.10,    // max 10% from last trade price
	})

	eng, err := dialEngine("tcp://127.0.0.1:9000")
	if err != nil {
		log.Fatalf("cannot connect to engine: %v", err)
	}

	h := newHub()

	// pass risk manager into sequencer
	go runSequencer(orderCh, seqCh, risk)
	go runEngineWriter(eng, seqCh)
	go runEngineReader(eng, fillCh)

	// update positions when fills come back
	go func() {
		for fill := range fillCh {
			risk.RecordFill(fill)
			h.broadcast(fill)
		}
	}()

	startServer(":8080", h, orderCh)
}