package main

// Order is what comes in from a WebSocket client as JSON
// json tags tell Go how to parse the field names from JSON
type Order struct {
	ID       uint64 `json:"id"`
	Side     string `json:"side"`   // "buy" or "sell"
	Price    uint64 `json:"price"`  // in cents — same as Rust engine
	Quantity uint64 `json:"quantity"`
}

// SequencedOrder is an Order that has been stamped with a sequence number
// by the sequencer before being sent to the Rust engine
type SequencedOrder struct {
	Order
	SeqNum uint64
}

// Fill is what comes back from the Rust engine when a match happens
// and what we broadcast back to WebSocket clients as JSON
type Fill struct {
	BuyOrderID  uint64 `json:"buy_order_id"`
	SellOrderID uint64 `json:"sell_order_id"`
	Price       uint64 `json:"price"`
	Quantity    uint64 `json:"quantity"`
	SeqNum      uint64 `json:"seq_num"`
}

// WireOrder is the binary format sent over the Unix socket to the Rust engine
// fixed size: 8+1+8+8+8 = 33 bytes per order
// fixed size means no length-prefix needed — we always read exactly 33 bytes
type WireOrder struct {
	ID       uint64
	Side     uint8  // 0 = buy, 1 = sell
	Price    uint64
	Quantity uint64
	SeqNum   uint64
}

// WireFill is the binary format that comes back from the Rust engine
// fixed size: 8+8+8+8 = 32 bytes per fill
type WireFill struct {
	BuyOrderID  uint64
	SellOrderID uint64
	Price       uint64
	Quantity    uint64
}