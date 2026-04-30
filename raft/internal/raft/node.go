package raft

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Node struct {
	mu sync.Mutex

	id        int
	peers     []int
	transport Transport

	role        Role
	currentTerm int
	votedFor    int

	log         []LogEntry
	commitIndex int
	lastApplied int
	applied     []string

	nextIndex  map[int]int
	matchIndex map[int]int

	electionResetAt time.Time
	electionTimeout time.Duration
}

func NewNode(id int, peers []int, transport Transport) *Node {
	n := &Node{
		id:          id,
		peers:       peers,
		transport:   transport,
		role:        Follower,
		commitIndex: -1,
		lastApplied: -1,
		nextIndex:   make(map[int]int),
		matchIndex:  make(map[int]int),
	}
	n.resetElectionTimerLocked()
	return n
}

func (n *Node) ID() int {
	return n.id
}

func (n *Node) State() (term int, role Role) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.currentTerm, n.role
}

func (n *Node) CommittedCommands() []string {
	n.mu.Lock()
	defer n.mu.Unlock()

	out := make([]string, len(n.applied))
	copy(out, n.applied)
	return out
}

func (n *Node) LogLength() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.log)
}

func (n *Node) Start(ctx context.Context) {
	go n.electionLoop(ctx)
}

func (n *Node) electionLoop(ctx context.Context) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.mu.Lock()
			role := n.role
			expired := time.Since(n.electionResetAt) >= n.electionTimeout
			n.mu.Unlock()

			if role != Leader && expired {
				n.startElection(ctx)
			}
		}
	}
}

func (n *Node) startElection(ctx context.Context) {
	n.mu.Lock()
	n.role = Candidate
	n.currentTerm++
	termStarted := n.currentTerm
	n.votedFor = n.id
	n.resetElectionTimerLocked()
	lastLogIndex, lastLogTerm := n.lastLogIndexTermLocked()
	n.mu.Unlock()

	votes := 1
	majority := (len(n.peers)+1)/2 + 1

	for _, peerID := range n.peers {
		peerID := peerID
		go func() {
			reply, err := n.transport.RequestVote(n.id, peerID, RequestVoteArgs{
				Term:         termStarted,
				CandidateID:  n.id,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
			})
			if err != nil {
				return
			}

			n.mu.Lock()
			defer n.mu.Unlock()

			if n.currentTerm != termStarted || n.role != Candidate {
				return
			}

			if reply.Term > n.currentTerm {
				n.becomeFollowerLocked(reply.Term)
				return
			}

			if reply.VoteGranted {
				votes++
				if votes >= majority && n.role == Candidate {
					n.role = Leader
					n.initializeLeaderStateLocked()
					n.resetElectionTimerLocked()
					go n.heartbeatLoop(ctx, termStarted)
				}
			}
		}()
	}
}

func (n *Node) heartbeatLoop(ctx context.Context, leaderTerm int) {
	n.sendHeartbeats(leaderTerm)

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.mu.Lock()
			stillLeader := n.role == Leader && n.currentTerm == leaderTerm
			n.mu.Unlock()

			if !stillLeader {
				return
			}

			n.sendHeartbeats(leaderTerm)
		}
	}
}

func (n *Node) Submit(command string) bool {
	n.mu.Lock()
	if n.role != Leader {
		n.mu.Unlock()
		return false
	}

	term := n.currentTerm
	n.log = append(n.log, LogEntry{
		Term:    term,
		Command: command,
	})
	entryIndex := len(n.log) - 1
	n.mu.Unlock()

	for _, peerID := range n.peers {
		go n.replicateToPeer(peerID, term)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		if n.role != Leader || n.currentTerm != term {
			n.mu.Unlock()
			return false
		}

		committed := n.commitIndex >= entryIndex
		n.mu.Unlock()

		if committed {
			n.sendHeartbeats(term)
			return true
		}

		time.Sleep(10 * time.Millisecond)
	}

	return false
}

func (n *Node) sendHeartbeats(leaderTerm int) {
	for _, peerID := range n.peers {
		go n.replicateToPeer(peerID, leaderTerm)
	}
}

func (n *Node) replicateToPeer(peerID int, leaderTerm int) {
	n.mu.Lock()
	if n.role != Leader || n.currentTerm != leaderTerm {
		n.mu.Unlock()
		return
	}

	nextIdx, ok := n.nextIndex[peerID]
	if !ok {
		nextIdx = len(n.log)
		n.nextIndex[peerID] = nextIdx
	}

	prevLogIndex := nextIdx - 1
	prevLogTerm := 0
	if prevLogIndex >= 0 {
		prevLogTerm = n.log[prevLogIndex].Term
	}

	entries := make([]LogEntry, len(n.log[nextIdx:]))
	copy(entries, n.log[nextIdx:])
	leaderCommit := n.commitIndex
	n.mu.Unlock()

	reply, err := n.transport.AppendEntries(n.id, peerID, AppendEntriesArgs{
		Term:         leaderTerm,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: leaderCommit,
	})
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.role != Leader || n.currentTerm != leaderTerm {
		return
	}

	if reply.Term > n.currentTerm {
		n.becomeFollowerLocked(reply.Term)
		return
	}

	if reply.Success {
		n.matchIndex[peerID] = prevLogIndex + len(entries)
		n.nextIndex[peerID] = n.matchIndex[peerID] + 1
		n.advanceCommitIndexLocked()
		return
	}

	if n.nextIndex[peerID] > 0 {
		n.nextIndex[peerID]--
	}
}

func (n *Node) handleRequestVote(args RequestVoteArgs) RequestVoteReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	if args.Term < n.currentTerm {
		return RequestVoteReply{
			Term:        n.currentTerm,
			VoteGranted: false,
		}
	}

	if args.Term > n.currentTerm {
		n.becomeFollowerLocked(args.Term)
	}

	if !n.isCandidateUpToDateLocked(args.LastLogIndex, args.LastLogTerm) {
		return RequestVoteReply{
			Term:        n.currentTerm,
			VoteGranted: false,
		}
	}

	grant := false
	if n.votedFor == 0 || n.votedFor == args.CandidateID {
		n.votedFor = args.CandidateID
		n.resetElectionTimerLocked()
		grant = true
	}

	return RequestVoteReply{
		Term:        n.currentTerm,
		VoteGranted: grant,
	}
}

func (n *Node) handleAppendEntries(args AppendEntriesArgs) AppendEntriesReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	if args.Term < n.currentTerm {
		return AppendEntriesReply{
			Term:    n.currentTerm,
			Success: false,
		}
	}

	if args.Term > n.currentTerm {
		n.currentTerm = args.Term
		n.votedFor = 0
	}

	n.role = Follower
	n.resetElectionTimerLocked()

	if args.PrevLogIndex >= 0 {
		if args.PrevLogIndex >= len(n.log) {
			return AppendEntriesReply{
				Term:    n.currentTerm,
				Success: false,
			}
		}
		if n.log[args.PrevLogIndex].Term != args.PrevLogTerm {
			return AppendEntriesReply{
				Term:    n.currentTerm,
				Success: false,
			}
		}
	}

	insertAt := args.PrevLogIndex + 1
	for i, entry := range args.Entries {
		idx := insertAt + i

		if idx < len(n.log) {
			if n.log[idx].Term != entry.Term || n.log[idx].Command != entry.Command {
				n.log = append(n.log[:idx], args.Entries[i:]...)
				break
			}
		} else {
			n.log = append(n.log, args.Entries[i:]...)
			break
		}
	}

	if args.LeaderCommit > n.commitIndex {
		lastIndex := len(n.log) - 1
		if args.LeaderCommit < lastIndex {
			n.commitIndex = args.LeaderCommit
		} else {
			n.commitIndex = lastIndex
		}
		n.applyEntriesLocked()
	}

	return AppendEntriesReply{
		Term:    n.currentTerm,
		Success: true,
	}
}

func (n *Node) becomeFollowerLocked(newTerm int) {
	n.role = Follower
	n.currentTerm = newTerm
	n.votedFor = 0
	n.resetElectionTimerLocked()
}

func (n *Node) initializeLeaderStateLocked() {
	n.nextIndex = make(map[int]int)
	n.matchIndex = make(map[int]int)

	next := len(n.log)
	for _, peerID := range n.peers {
		n.nextIndex[peerID] = next
		n.matchIndex[peerID] = -1
	}
}

func (n *Node) resetElectionTimerLocked() {
	n.electionResetAt = time.Now()
	n.electionTimeout = ElectionMin + time.Duration(rand.Int63n(int64(ElectionJitter)))
}

func (n *Node) lastLogIndexTermLocked() (int, int) {
	if len(n.log) == 0 {
		return -1, 0
	}
	lastIdx := len(n.log) - 1
	return lastIdx, n.log[lastIdx].Term
}

func (n *Node) isCandidateUpToDateLocked(candidateLastIndex, candidateLastTerm int) bool {
	myLastIndex, myLastTerm := n.lastLogIndexTermLocked()

	if candidateLastTerm != myLastTerm {
		return candidateLastTerm > myLastTerm
	}

	return candidateLastIndex >= myLastIndex
}

func (n *Node) advanceCommitIndexLocked() {
	majority := (len(n.peers)+1)/2 + 1

	for idx := len(n.log) - 1; idx > n.commitIndex; idx-- {
		replicated := 1 // leader itself

		for _, peerID := range n.peers {
			if n.matchIndex[peerID] >= idx {
				replicated++
			}
		}

		if replicated >= majority {
			n.commitIndex = idx
			n.applyEntriesLocked()
			return
		}
	}
}

func (n *Node) applyEntriesLocked() {
	for n.lastApplied < n.commitIndex {
		n.lastApplied++
		n.applied = append(n.applied, n.log[n.lastApplied].Command)
	}
}