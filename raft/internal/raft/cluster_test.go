package raft

import (
	"context"
	"testing"
	"time"
)

func makeCluster(t *testing.T, size int) (context.Context, context.CancelFunc, *LocalTransport, []*Node) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	transport := NewLocalTransport(5 * time.Millisecond)

	allIDs := make([]int, 0, size)
	for i := 1; i <= size; i++ {
		allIDs = append(allIDs, i)
	}

	nodes := make([]*Node, 0, size)
	for i := 1; i <= size; i++ {
		peers := make([]int, 0, size-1)
		for _, id := range allIDs {
			if id != i {
				peers = append(peers, id)
			}
		}

		node := NewNode(i, peers, transport)
		transport.Register(node)
		nodes = append(nodes, node)
	}

	for _, node := range nodes {
		node.Start(ctx)
	}

	return ctx, cancel, transport, nodes
}

func countLeaders(nodes []*Node) []int {
	leaders := []int{}
	for _, n := range nodes {
		_, role := n.State()
		if role == Leader {
			leaders = append(leaders, n.ID())
		}
	}
	return leaders
}

func waitForSingleLeader(nodes []*Node, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		leaders := countLeaders(nodes)
		if len(leaders) == 1 {
			return leaders[0], true
		}
		time.Sleep(50 * time.Millisecond)
	}

	return 0, false
}

func findNodeByID(nodes []*Node, id int) *Node {
	for _, n := range nodes {
		if n.ID() == id {
			return n
		}
	}
	return nil
}

func waitForAllCommitted(nodes []*Node, command string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allGood := true

		for _, n := range nodes {
			cmds := n.CommittedCommands()
			if len(cmds) != 1 || cmds[0] != command {
				allGood = false
				break
			}
		}

		if allGood {
			return true
		}

		time.Sleep(50 * time.Millisecond)
	}

	return false
}

func waitForExactCommitted(nodes []*Node, expected []string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allGood := true

		for _, n := range nodes {
			cmds := n.CommittedCommands()
			if len(cmds) != len(expected) {
				allGood = false
				break
			}
			for i := range expected {
				if cmds[i] != expected[i] {
					allGood = false
					break
				}
			}
			if !allGood {
				break
			}
		}

		if allGood {
			return true
		}

		time.Sleep(50 * time.Millisecond)
	}

	return false
}

func TestLeaderElection(t *testing.T) {
	_, cancel, _, nodes := makeCluster(t, 3)
	defer cancel()

	leaderID, ok := waitForSingleLeader(nodes, 3*time.Second)
	if !ok {
		t.Fatalf("expected exactly one leader within timeout")
	}

	t.Logf("leader elected: node %d", leaderID)

	time.Sleep(600 * time.Millisecond)

	leaders := countLeaders(nodes)
	if len(leaders) != 1 {
		t.Fatalf("expected exactly one stable leader, got %v", leaders)
	}
}

func TestSingleEntryReplication(t *testing.T) {
	_, cancel, _, nodes := makeCluster(t, 3)
	defer cancel()

	leaderID, ok := waitForSingleLeader(nodes, 3*time.Second)
	if !ok {
		t.Fatalf("expected leader within timeout")
	}

	leader := findNodeByID(nodes, leaderID)
	if leader == nil {
		t.Fatalf("leader node not found")
	}

	ok = leader.Submit("order-1")
	if !ok {
		t.Fatalf("leader failed to replicate command")
	}

	ok = waitForAllCommitted(nodes, "order-1", 2*time.Second)
	if !ok {
		for _, n := range nodes {
			t.Logf("node=%d committed=%v log_len=%d", n.ID(), n.CommittedCommands(), n.LogLength())
		}
		t.Fatalf("expected all nodes to commit the same command")
	}
}

func TestRequestVoteRejectsStaleCandidateLog(t *testing.T) {
	transport := NewLocalTransport(0)

	voter := NewNode(1, []int{2}, transport)
	voter.currentTerm = 3
	voter.log = []LogEntry{
		{Term: 1, Command: "cmd-1"},
		{Term: 3, Command: "cmd-2"},
	}

	reply := voter.handleRequestVote(RequestVoteArgs{
		Term:         3,
		CandidateID:  2,
		LastLogIndex: 0,
		LastLogTerm:  1,
	})

	if reply.VoteGranted {
		t.Fatalf("expected stale candidate log to be rejected")
	}
}

func TestRequestVoteGrantsUpToDateCandidateLog(t *testing.T) {
	transport := NewLocalTransport(0)

	voter := NewNode(1, []int{2}, transport)
	voter.currentTerm = 3
	voter.log = []LogEntry{
		{Term: 1, Command: "cmd-1"},
		{Term: 3, Command: "cmd-2"},
	}

	reply := voter.handleRequestVote(RequestVoteArgs{
		Term:         3,
		CandidateID:  2,
		LastLogIndex: 1,
		LastLogTerm:  3,
	})

	if !reply.VoteGranted {
		t.Fatalf("expected up-to-date candidate log to be granted")
	}
}

func TestLeaderFailoverPreservesCommittedLog(t *testing.T) {
	_, cancel, transport, nodes := makeCluster(t, 3)
	defer cancel()

	leaderID, ok := waitForSingleLeader(nodes, 3*time.Second)
	if !ok {
		t.Fatalf("expected initial leader within timeout")
	}

	initialLeader := findNodeByID(nodes, leaderID)
	if initialLeader == nil {
		t.Fatalf("initial leader not found")
	}

	ok = initialLeader.Submit("order-1")
	if !ok {
		t.Fatalf("initial leader failed to commit order-1")
	}

	ok = waitForAllCommitted(nodes, "order-1", 2*time.Second)
	if !ok {
		t.Fatalf("expected all nodes to commit order-1 before failover")
	}

	transport.Disconnect(initialLeader.ID())
	t.Logf("disconnected old leader: node %d", initialLeader.ID())

	survivors := make([]*Node, 0, 2)
	for _, n := range nodes {
		if n.ID() != initialLeader.ID() {
			survivors = append(survivors, n)
		}
	}

	newLeaderID, ok := waitForSingleLeader(survivors, 3*time.Second)
	if !ok {
		for _, n := range survivors {
			term, role := n.State()
			t.Logf("survivor node=%d term=%d role=%s committed=%v", n.ID(), term, role, n.CommittedCommands())
		}
		t.Fatalf("expected new leader among surviving nodes")
	}

	if newLeaderID == initialLeader.ID() {
		t.Fatalf("expected a different leader after disconnect")
	}

	newLeader := findNodeByID(survivors, newLeaderID)
	if newLeader == nil {
		t.Fatalf("new leader not found")
	}

	ok = newLeader.Submit("order-2")
	if !ok {
		t.Fatalf("new leader failed to commit order-2")
	}

	ok = waitForExactCommitted(survivors, []string{"order-1", "order-2"}, 2*time.Second)
	if !ok {
		for _, n := range survivors {
			t.Logf("survivor node=%d committed=%v log_len=%d", n.ID(), n.CommittedCommands(), n.LogLength())
		}
		t.Fatalf("expected surviving majority to preserve order-1 and commit order-2")
	}

	oldLeaderCommitted := initialLeader.CommittedCommands()
	if len(oldLeaderCommitted) == 0 || oldLeaderCommitted[0] != "order-1" {
		t.Fatalf("expected old leader to retain already-committed order-1, got %v", oldLeaderCommitted)
	}
}

func TestFollowerReconnectCatchesUp(t *testing.T) {
	_, cancel, transport, nodes := makeCluster(t, 3)
	defer cancel()

	leaderID, ok := waitForSingleLeader(nodes, 3*time.Second)
	if !ok {
		t.Fatalf("expected leader within timeout")
	}

	leader := findNodeByID(nodes, leaderID)
	if leader == nil {
		t.Fatalf("leader node not found")
	}

	// First commit on all nodes.
	ok = leader.Submit("order-1")
	if !ok {
		t.Fatalf("leader failed to commit order-1")
	}

	ok = waitForExactCommitted(nodes, []string{"order-1"}, 2*time.Second)
	if !ok {
		for _, n := range nodes {
			t.Logf("node=%d committed=%v log_len=%d", n.ID(), n.CommittedCommands(), n.LogLength())
		}
		t.Fatalf("expected all nodes to commit order-1")
	}

	// Pick one follower to lag behind.
	var lagging *Node
	for _, n := range nodes {
		if n.ID() != leader.ID() {
			lagging = n
			break
		}
	}
	if lagging == nil {
		t.Fatalf("failed to choose lagging follower")
	}

	// Keep it from starting an election while disconnected.
	lagging.mu.Lock()
	lagging.electionTimeout = 2 * time.Second
	lagging.electionResetAt = time.Now()
	lagging.mu.Unlock()

	transport.Disconnect(lagging.ID())
	t.Logf("disconnected lagging follower: node %d", lagging.ID())

	// Commit second entry on the majority only.
	ok = leader.Submit("order-2")
	if !ok {
		t.Fatalf("leader failed to commit order-2 with one follower disconnected")
	}

	majorityNodes := make([]*Node, 0, 2)
	for _, n := range nodes {
		if n.ID() != lagging.ID() {
			majorityNodes = append(majorityNodes, n)
		}
	}

	ok = waitForExactCommitted(majorityNodes, []string{"order-1", "order-2"}, 2*time.Second)
	if !ok {
		for _, n := range majorityNodes {
			t.Logf("majority node=%d committed=%v log_len=%d", n.ID(), n.CommittedCommands(), n.LogLength())
		}
		t.Fatalf("expected majority to commit order-2 while follower is disconnected")
	}

	transport.Reconnect(lagging.ID())
	t.Logf("reconnected lagging follower: node %d", lagging.ID())

	ok = waitForExactCommitted(nodes, []string{"order-1", "order-2"}, 3*time.Second)
	if !ok {
		for _, n := range nodes {
			t.Logf("node=%d committed=%v log_len=%d", n.ID(), n.CommittedCommands(), n.LogLength())
		}
		t.Fatalf("expected reconnected follower to catch up")
	}
}

func TestMultipleSequentialEntriesReplication(t *testing.T) {
	_, cancel, _, nodes := makeCluster(t, 3)
	defer cancel()

	leaderID, ok := waitForSingleLeader(nodes, 3*time.Second)
	if !ok {
		t.Fatalf("expected leader within timeout")
	}

	leader := findNodeByID(nodes, leaderID)
	if leader == nil {
		t.Fatalf("leader node not found")
	}

	commands := []string{"order-1", "order-2", "order-3", "order-4"}

	for _, cmd := range commands {
		ok = leader.Submit(cmd)
		if !ok {
			t.Fatalf("leader failed to submit %s", cmd)
		}
	}

	ok = waitForExactCommitted(nodes, commands, 3*time.Second)
	if !ok {
		for _, n := range nodes {
			t.Logf("node=%d committed=%v log_len=%d", n.ID(), n.CommittedCommands(), n.LogLength())
		}
		t.Fatalf("expected all nodes to replicate the exact multi-entry sequence")
	}
}

func TestFailoverAfterMultipleCommittedEntries(t *testing.T) {
	_, cancel, transport, nodes := makeCluster(t, 3)
	defer cancel()

	leaderID, ok := waitForSingleLeader(nodes, 3*time.Second)
	if !ok {
		t.Fatalf("expected initial leader within timeout")
	}

	initialLeader := findNodeByID(nodes, leaderID)
	if initialLeader == nil {
		t.Fatalf("initial leader not found")
	}

	initialCommands := []string{"order-1", "order-2", "order-3"}
	for _, cmd := range initialCommands {
		ok = initialLeader.Submit(cmd)
		if !ok {
			t.Fatalf("initial leader failed to submit %s", cmd)
		}
	}

	ok = waitForExactCommitted(nodes, initialCommands, 3*time.Second)
	if !ok {
		for _, n := range nodes {
			t.Logf("node=%d committed=%v log_len=%d", n.ID(), n.CommittedCommands(), n.LogLength())
		}
		t.Fatalf("expected all nodes to commit initial multi-entry log")
	}

	transport.Disconnect(initialLeader.ID())
	t.Logf("disconnected old leader: node %d", initialLeader.ID())

	survivors := make([]*Node, 0, 2)
	for _, n := range nodes {
		if n.ID() != initialLeader.ID() {
			survivors = append(survivors, n)
		}
	}

	newLeaderID, ok := waitForSingleLeader(survivors, 3*time.Second)
	if !ok {
		for _, n := range survivors {
			term, role := n.State()
			t.Logf("survivor node=%d term=%d role=%s committed=%v", n.ID(), term, role, n.CommittedCommands())
		}
		t.Fatalf("expected new leader among survivors")
	}

	if newLeaderID == initialLeader.ID() {
		t.Fatalf("expected a different leader after disconnect")
	}

	newLeader := findNodeByID(survivors, newLeaderID)
	if newLeader == nil {
		t.Fatalf("new leader not found")
	}

	ok = newLeader.Submit("order-4")
	if !ok {
		t.Fatalf("new leader failed to submit order-4")
	}

	expected := []string{"order-1", "order-2", "order-3", "order-4"}

	ok = waitForExactCommitted(survivors, expected, 3*time.Second)
	if !ok {
		for _, n := range survivors {
			t.Logf("survivor node=%d committed=%v log_len=%d", n.ID(), n.CommittedCommands(), n.LogLength())
		}
		t.Fatalf("expected survivors to preserve prior log and append order-4")
	}

	oldLeaderCommitted := initialLeader.CommittedCommands()
	if len(oldLeaderCommitted) != 3 {
		t.Fatalf("expected disconnected old leader to retain first 3 committed commands, got %v", oldLeaderCommitted)
	}
	for i, cmd := range initialCommands {
		if oldLeaderCommitted[i] != cmd {
			t.Fatalf("expected old leader command %d to be %s, got %s", i, cmd, oldLeaderCommitted[i])
		}
	}
}