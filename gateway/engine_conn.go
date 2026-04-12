package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
)

// engineConn manages the connection to the Rust matching engine
// over a Unix domain socket
type engineConn struct {
	conn net.Conn
}

// dialEngine connects to the Rust engine's Unix socket
func dialEngine(addr string) (*engineConn, error) {
    conn, err := net.Dial("tcp", "127.0.0.1:9000")
    if err != nil {
        return nil, fmt.Errorf("failed to connect to engine: %w", err)
    }
    log.Printf("connected to Rust engine at %s", addr)
    return &engineConn{conn: conn}, nil
}

// sendOrder serializes a SequencedOrder to binary and writes it to the socket
// wire format: ID(8) Side(1) Price(8) Quantity(8) SeqNum(8) = 33 bytes
func (e *engineConn) sendOrder(o SequencedOrder) error {
	buf := make([]byte, 33)

	var side uint8
	if o.Side == "sell" {
		side = 1
	}

	binary.LittleEndian.PutUint64(buf[0:8], o.ID)
	buf[8] = side
	binary.LittleEndian.PutUint64(buf[9:17], o.Price)
	binary.LittleEndian.PutUint64(buf[17:25], o.Quantity)
	binary.LittleEndian.PutUint64(buf[25:33], o.SeqNum)

	_, err := e.conn.Write(buf)
	return err
}

// readFill reads exactly 32 bytes from the socket and deserializes a Fill
// wire format: BuyOrderID(8) SellOrderID(8) Price(8) Quantity(8) = 32 bytes
func (e *engineConn) readFill() (WireFill, error) {
	buf := make([]byte, 32)
	_, err := readFull(e.conn, buf)
	if err != nil {
		return WireFill{}, err
	}

	return WireFill{
		BuyOrderID:  binary.LittleEndian.Uint64(buf[0:8]),
		SellOrderID: binary.LittleEndian.Uint64(buf[8:16]),
		Price:       binary.LittleEndian.Uint64(buf[16:24]),
		Quantity:    binary.LittleEndian.Uint64(buf[24:32]),
	}, nil
}

// readFull reads exactly len(buf) bytes — keeps reading until the buffer is full
// net.Conn.Read is not guaranteed to fill the buffer in one call
func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// runEngineWriter reads sequenced orders from orderCh and sends them to the engine
func runEngineWriter(eng *engineConn, orderCh <-chan SequencedOrder) {
	for order := range orderCh {
		if err := eng.sendOrder(order); err != nil {
			log.Printf("error sending order to engine: %v", err)
			return
		}
	}
}

// runEngineReader reads fills from the engine and broadcasts them on fillCh
func runEngineReader(eng *engineConn, fillCh chan<- Fill) {
	for {
		wf, err := eng.readFill()
		if err != nil {
			log.Printf("error reading fill from engine: %v", err)
			return
		}

		fillCh <- Fill{
			BuyOrderID:  wf.BuyOrderID,
			SellOrderID: wf.SellOrderID,
			Price:       wf.Price,
			Quantity:    wf.Quantity,
		}
	}
}