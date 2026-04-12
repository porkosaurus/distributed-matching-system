package main

import (
	"log"
)

func main() {
	// channel buffer sizes — how many items can queue up before blocking
	// 1000 is generous for development
	orderCh := make(chan Order, 1000)
	seqCh   := make(chan SequencedOrder, 1000)
	fillCh  := make(chan Fill, 1000)

	// connect to the Rust engine over Unix socket
	// this path must match what the Rust engine listens on
	eng, err := dialEngine("tcp://127.0.0.1:9000")
	if err != nil {
		log.Fatalf("cannot connect to engine: %v", err)
	}

	// create the client hub for WebSocket broadcast
	h := newHub()

	// start all goroutines
	// each runs independently and communicates only through channels
	go runSequencer(orderCh, seqCh)
	go runEngineWriter(eng, seqCh)
	go runEngineReader(eng, fillCh)
	go runBroadcaster(h, fillCh)

	// startServer blocks forever — it's the main loop
	// all the real work happens in the goroutines above
	startServer(":8080", h, orderCh)
}