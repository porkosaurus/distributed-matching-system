package main

import (
	"context"
	"fmt"
	"time"

	"matching-engine/raft/internal/raft"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	transport := raft.NewLocalTransport(10 * time.Millisecond)

	node1 := raft.NewNode(1, []int{2, 3}, transport)
	node2 := raft.NewNode(2, []int{1, 3}, transport)
	node3 := raft.NewNode(3, []int{1, 2}, transport)

	transport.Register(node1)
	transport.Register(node2)
	transport.Register(node3)

	node1.Start(ctx)
	node2.Start(ctx)
	node3.Start(ctx)

	nodes := []*raft.Node{node1, node2, node3}

	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		fmt.Println("---- cluster state ----")
		for _, n := range nodes {
			term, role := n.State()
			fmt.Printf("node=%d term=%d role=%s\n", n.ID(), term, role)
		}
	}
}