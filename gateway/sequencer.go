package main

import "log"

// runSequencer stamps orders with sequence numbers after risk checks pass.
// Orders that fail risk checks are logged and dropped.
func runSequencer(inCh <-chan Order, outCh chan<- SequencedOrder, risk *RiskManager) {
	var seq uint64 = 0

	for order := range inCh {
		// run risk checks before sequencing
		if err := risk.CheckOrder(order); err != nil {
			log.Printf("order %d rejected by risk: %v", order.ID, err)
			continue // drop the order, don't sequence it
		}

		seq++
		outCh <- SequencedOrder{
			Order:  order,
			SeqNum: seq,
		}
	}
}