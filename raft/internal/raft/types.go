package raft

import "time"

type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

func (r Role) String() string {
	switch r {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

type LogEntry struct {
	Term    int
	Command string
}

type RequestVoteArgs struct {
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         int
	LeaderID     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

type Transport interface {
	RequestVote(fromID, toID int, args RequestVoteArgs) (RequestVoteReply, error)
	AppendEntries(fromID, toID int, args AppendEntriesArgs) (AppendEntriesReply, error)
}

const (
	HeartbeatInterval = 100 * time.Millisecond
	ElectionMin       = 300 * time.Millisecond
	ElectionJitter    = 250 * time.Millisecond
)