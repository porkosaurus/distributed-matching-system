package raft

import (
	"fmt"
	"sync"
	"time"
)

type LocalTransport struct {
	mu           sync.RWMutex
	nodes        map[int]*Node
	latency      time.Duration
	disconnected map[int]bool
}

func NewLocalTransport(latency time.Duration) *LocalTransport {
	return &LocalTransport{
		nodes:        make(map[int]*Node),
		latency:      latency,
		disconnected: make(map[int]bool),
	}
}

func (t *LocalTransport) Register(node *Node) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nodes[node.id] = node
}

func (t *LocalTransport) Disconnect(nodeID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.disconnected[nodeID] = true
}

func (t *LocalTransport) Reconnect(nodeID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.disconnected, nodeID)
}

func (t *LocalTransport) isDisconnected(nodeID int) bool {
	return t.disconnected[nodeID]
}

func (t *LocalTransport) RequestVote(fromID, toID int, args RequestVoteArgs) (RequestVoteReply, error) {
	t.mu.RLock()
	node, ok := t.nodes[toID]
	fromDown := t.isDisconnected(fromID)
	toDown := t.isDisconnected(toID)
	t.mu.RUnlock()

	if !ok {
		return RequestVoteReply{}, fmt.Errorf("node %d not found", toID)
	}
	if fromDown || toDown {
		return RequestVoteReply{}, fmt.Errorf("request vote blocked: from=%d to=%d", fromID, toID)
	}

	if t.latency > 0 {
		time.Sleep(t.latency)
	}

	return node.handleRequestVote(args), nil
}

func (t *LocalTransport) AppendEntries(fromID, toID int, args AppendEntriesArgs) (AppendEntriesReply, error) {
	t.mu.RLock()
	node, ok := t.nodes[toID]
	fromDown := t.isDisconnected(fromID)
	toDown := t.isDisconnected(toID)
	t.mu.RUnlock()

	if !ok {
		return AppendEntriesReply{}, fmt.Errorf("node %d not found", toID)
	}
	if fromDown || toDown {
		return AppendEntriesReply{}, fmt.Errorf("append entries blocked: from=%d to=%d", fromID, toID)
	}

	if t.latency > 0 {
		time.Sleep(t.latency)
	}

	return node.handleAppendEntries(args), nil
}