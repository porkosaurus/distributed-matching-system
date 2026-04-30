#pragma once
#include <string>
#include <vector>

enum class Role{
    Follower,
    Candidate,
    Leader
};

struct LogEntry{
    int term;
    std::string command;
};

struct RequestVoteArgs{
    int term;
    int candidateId;
    int lastLogIndex;
    int lastLogTerm;
};

struct RequestVoteReply{
    int term;
    bool voteGranted;
};

struct AppendEntriesArgs {
    int term;
    int leaderId;
    int prevLogIndex;
    int prevLogTerm;
    std::vector<LogEntry> entries;
    int leaderCommit;
};

struct AppendEntriesReply {
    int term;
    bool success;
};