package main

// Sequencer owns a counter and stamps every order with the next sequence number.
// It runs as a single goroutine — one goroutine owns the counter, so no lock needed.
// Orders arrive on inCh from WebSocket goroutines.
// Sequenced orders go out on outCh to the engine connection goroutine.
func runSequencer(inCh <-chan Order, outCh chan<- SequencedOrder) {
	var seq uint64 = 0

	for order := range inCh {
		seq++
		outCh <- SequencedOrder{
			Order:  order,
			SeqNum: seq,
		}
	}
}