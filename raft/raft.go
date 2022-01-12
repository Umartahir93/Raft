package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"PS_2/labrpc"
	"bytes"
	"encoding/gob"
	"sort"
	"time"
)

import "sync"
import "math/rand"

const (
	//Tester limits 10 heart beats per second, i.e. using timeouts 100-350 (Requirement Given in Assignment 2)
	beatIntervalMs        = 100
	electionTimeoutMs     = 300
	follower          int = iota
	candidate
	leader
)

// ApplyMsg
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make().
//
type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool
	Snapshot    []byte
}

// Raft
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // index into peers[]

	muForMatchIndex  sync.Mutex //Lock to protect shared access to matchIndex
	muForPersist     sync.Mutex //Lock to protect persist
	muForLog         sync.Mutex //Lock to protect Log
	muForNextIndex   sync.Mutex //Lock to protect nextIndex
	muForTerm        sync.Mutex //Lock to protect term
	muForVotedFor    sync.Mutex //Lock to protect term
	muForCommitIndex sync.Mutex //Lock to protect commit index

	// Your data here.
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	Term        int   //current term of the server
	VotedFor    int   //candidateId that received vote
	Log         []Log //contain commands of server with terms
	commitIndex int   //index of the highest entry known to be committed

	//Reinitialized after election
	nextIndex  []int //index of the next log entry to send to that server
	matchIndex []int //index of the highest log entry known to be replicated on server

	timeout                 *time.Timer
	followerAppendEntries   chan bool
	followerChange          chan FollowerChange
	followerChangeCompleted chan bool
	receivedQuit            chan bool
	checkQuitGoRoutine      chan bool
	applyCh                 chan ApplyMsg
	assumeRole              int
	killed                  bool
}

// Log As stated in paper this log entry will contain command for state machine
//and term when that command was received by leader
type Log struct {
	Term    int
	Command interface{}
}

// AppendLogRequest request
type AppendLogRequest struct {
	CurrentTerm  int
	PrevLogTerm  int
	LeaderId     int
	PrevLogIndex int
	Logs         []Log
	CommitIndex  int
}

//AppendLogReply reply
type AppendLogReply struct {
	SuccessTerm int
	Success     bool
	Index       int
	Term        int
}

// GetState return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here.
	rf.muForTerm.Lock()
	term = rf.Term
	rf.muForTerm.Unlock()
	rf.mu.Lock()
	isleader = rf.assumeRole == leader
	rf.mu.Unlock()
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here.
	rf.muForPersist.Lock()
	defer rf.muForPersist.Unlock()
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)

	//rf.muForTerm.Lock()
	err := e.Encode(rf.Term)
	//rf.muForTerm.Unlock()

	if err != nil {
		return
	}
	rf.muForVotedFor.Lock()
	err = e.Encode(rf.VotedFor)
	rf.muForVotedFor.Unlock()
	if err != nil {
		return
	}
	rf.muForLog.Lock()
	err = e.Encode(rf.Log)
	rf.muForLog.Unlock()
	if err != nil {
		return
	}
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	// Your code here.
	if data != nil && len(data) >= 1 {
		rf.muForPersist.Lock()
		r := bytes.NewBuffer(data)
		d := gob.NewDecoder(r)
		err := d.Decode(&rf.Term)
		if err != nil {
			return
		}
		rf.muForVotedFor.Lock()
		err = d.Decode(&rf.VotedFor)
		rf.muForVotedFor.Unlock()
		if err != nil {
			return
		}
		rf.muForLog.Lock()
		err = d.Decode(&rf.Log)
		rf.muForLog.Unlock()
		if err != nil {
			return
		}
		rf.muForPersist.Unlock()
	}
}

// RequestVoteArgs
// example RequestVote RPC arguments structure.
//
type RequestVoteArgs struct {
	// Your data here.
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// RequestVoteReply
// example RequestVote RPC reply structure.
//
type RequestVoteReply struct {
	// Your data here.
	Term        int
	VoteGranted bool
}

// RequestVote
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here.
	argsTermLessThanTerm := false
	rf.muForTerm.Lock()
	if args.Term < rf.Term {
		argsTermLessThanTerm = true
	}
	rf.muForTerm.Unlock()

	if argsTermLessThanTerm {
		rf.muForTerm.Lock()
		reply.Term = rf.Term
		reply.VoteGranted = false
		rf.muForTerm.Unlock()
		return
	} else {
		rf.checkRaftGrantVoteCondition(args, reply, rf.Log[len(rf.Log)-1].Term)
		rf.muForTerm.Lock()
		if rf.Term < args.Term {
			rf.followerChange <- FollowerChange{args.Term, args.CandidateId, reply.VoteGranted}
			<-rf.followerChangeCompleted
		}
		rf.muForTerm.Unlock()
	}
}

//In this method are conditions that should be followed to grant vote.
func (rf *Raft) checkRaftGrantVoteCondition(args RequestVoteArgs, reply *RequestVoteReply, lastLogTerm int) {
	rf.muForTerm.Lock()
	if args.LastLogTerm < lastLogTerm || (args.LastLogTerm == lastLogTerm && args.LastLogIndex < len(rf.Log)-1) {
		reply.Term = args.Term
		reply.VoteGranted = false

	} else if rf.Term >= args.Term {
		reply.Term = rf.Term
		reply.VoteGranted = false

	} else {
		reply.Term = args.Term
		reply.VoteGranted = true
	}
	rf.muForTerm.Unlock()
}

// FollowerChange This interface maintains the state of raft server when change to follower process gets initiated
type FollowerChange struct {
	term     int  //term of follower
	votedFor int  //voted for candidate
	reset    bool //flag used to reset timer
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// returns true if labrpc says the RPC was delivered.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// Start
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := false
	rf.mu.Lock()
	if rf.assumeRole == leader {
		isLeader = true
	}
	rf.mu.Unlock()

	if isLeader {
		rf.mu.Lock()
		term = rf.Term

		rf.muForLog.Lock()
		rf.Log = append(rf.Log, Log{term, command})
		rf.muForLog.Unlock()

		rf.muForNextIndex.Lock()
		index = rf.nextIndex[rf.me]
		rf.nextIndex[rf.me]++
		rf.muForMatchIndex.Lock()
		rf.matchIndex[rf.me] = rf.nextIndex[rf.me] - 1
		rf.muForMatchIndex.Unlock()
		rf.muForNextIndex.Unlock()

		rf.persist()
		rf.mu.Unlock()
		return index, term, true

	} else {
		return index, term, isLeader
	}
}

// Kill
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
	rf.mu.Lock()
	rf.killed = true
	rf.mu.Unlock()
}

// Make
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here.
	rf.receivedQuit = make(chan bool)
	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))
	rf.checkQuitGoRoutine = make(chan bool)
	rf.followerAppendEntries = make(chan bool)
	rf.followerChangeCompleted = make(chan bool)
	rf.followerChange = make(chan FollowerChange)

	rf.Term = 0
	rf.VotedFor = -1
	rf.applyCh = applyCh
	rf.Log = append(rf.Log, Log{-1, rf.Term})
	for i := 0; i < len(rf.peers); i++ {
		rf.muForNextIndex.Lock()
		rf.nextIndex[i] = 1
		rf.muForNextIndex.Unlock()
	}

	rf.readPersist(persister.ReadRaftState())
	randNo := rand.New(rand.NewSource(time.Now().UnixNano()))
	timeout := randNo.Intn(electionTimeoutMs) + electionTimeoutMs
	rf.timeout = time.NewTimer(time.Duration(timeout) * time.Millisecond)

	go rf.assumeFollowerRole()
	go rf.SetCommitIndex()

	return rf
}

func (rf *Raft) assumeFollowerRole() {
	rf.mu.Lock()
	rf.assumeRole = follower
	rf.mu.Unlock()
	var endFunction = false
	for {
		select {
		case <-rf.timeout.C:
			{
				go rf.assumeCandidateRole()
				endFunction = true
				break
			}

		case follower := <-rf.followerChange:
			{
				if follower.term > rf.Term {
					go rf.ToFollower(follower)
					endFunction = true
					break
				} else {
					rf.followerChangeCompleted <- true
					if follower.reset {
						randNo := rand.New(rand.NewSource(time.Now().UnixNano()))
						timeout := randNo.Intn(electionTimeoutMs) + electionTimeoutMs
						rf.timeout.Reset(time.Duration(timeout) * time.Millisecond)
					}
				}
			}

		case <-rf.receivedQuit:
			endFunction = true
			break
		}

		if endFunction == true {
			break
		}
	}
}

func (rf *Raft) assumeCandidateRole() {
	rf.mu.Lock()
	rf.assumeRole = candidate
	rf.mu.Unlock()
	var endFunction = false
	for {
		results := make(chan bool, len(rf.peers))
		go rf.initiateElections(results)
		randNo := rand.New(rand.NewSource(time.Now().UnixNano()))
		timeout := randNo.Intn(electionTimeoutMs) + electionTimeoutMs
		rf.timeout.Reset(time.Duration(timeout) * time.Millisecond)

		select {
		case <-rf.timeout.C:
			{
				continue
			}

		case result := <-results:
			{
				if result {
					go rf.assumeLeaderRole()
					go rf.leaderRoleCoreFunction()
					endFunction = true
					break
				}
			}

		case followerChange := <-rf.followerChange:
			{
				go rf.ToFollower(followerChange)
				endFunction = true
				break
			}

		case <-rf.receivedQuit:
			{
				endFunction = true
				break
			}
		}

		if endFunction == true {
			break
		}

	}
}

func (rf *Raft) initiateElections(resultChan chan bool) {
	rf.mu.Lock()
	if rf.assumeRole != candidate {
		rf.mu.Unlock()
		return
	} else {
		rf.muForTerm.Lock()
		rf.Term++
		rf.muForTerm.Unlock()
		rf.muForVotedFor.Lock()
		rf.VotedFor = rf.me
		rf.muForVotedFor.Unlock()
		rf.persist()

		votes := make([]bool, len(rf.peers))
		votes[rf.me] = true

		var request RequestVoteArgs
		rf.muForTerm.Lock()
		request.Term = rf.Term
		rf.muForTerm.Unlock()
		request.CandidateId = rf.me
		rf.muForLog.Lock()
		request.LastLogIndex = len(rf.Log) - 1
		request.LastLogTerm = rf.Log[request.LastLogIndex].Term
		rf.muForLog.Unlock()
		rf.mu.Unlock()

		rf.sendRPCRequestToOtherPeers(resultChan, request, votes)
	}

}

func (rf *Raft) sendRPCRequestToOtherPeers(result chan bool, request RequestVoteArgs, votes []bool) {
	for peer := 0; peer < len(rf.peers); peer++ {
		if peer != rf.me {
			go rf.requestForVote(result, request, votes, peer)
		}
	}
}

func (rf *Raft) requestForVote(result chan bool, request RequestVoteArgs, votes []bool, peer int) {
	count := 0
	var voteReply RequestVoteReply
	ok := rf.sendRequestVote(peer, request, &voteReply)
	rf.mu.Lock()
	votes[peer] = ok && voteReply.VoteGranted
	for i := 0; i < len(rf.peers); i++ {
		if votes[i] {
			count++
		}
	}
	rf.mu.Unlock()
	if count >= (len(rf.peers)/2 + 1) {
		result <- true
	}
}

func (rf *Raft) assumeLeaderRole() {
	rf.mu.Lock()
	if rf.assumeRole != candidate {
		rf.mu.Unlock()
		return
	}

	for i := 0; i < len(rf.nextIndex); i++ {
		rf.muForNextIndex.Lock()
		rf.nextIndex[i] = len(rf.Log)
		rf.muForNextIndex.Unlock()
	}
	rf.matchIndex[rf.me] = len(rf.Log) - 1
	rf.assumeRole = leader
	rf.mu.Unlock()
}

func (rf *Raft) leaderRoleCoreFunction() {
	var endFunction = false
	for {
		select {
		case <-rf.receivedQuit:
			{
				endFunction = true
				break
			}

		case followerChange := <-rf.followerChange:
			{
				go rf.ToFollower(followerChange)
				endFunction = true
				break
			}

		default:
			{
				isLeader := false
				rf.mu.Lock()
				if rf.assumeRole == leader {
					isLeader = true
				}
				rf.mu.Unlock()
				if isLeader {
					rf.synchronizeLogsWithAllPeers()
					time.Sleep(beatIntervalMs * time.Millisecond)
				}

			}
		}

		if endFunction == true {
			break
		}
	}
}

func (rf *Raft) ToFollower(followerChange FollowerChange) {
	if followerChange.votedFor != -1 {
		rf.muForVotedFor.Lock()
		rf.VotedFor = followerChange.votedFor
		rf.muForVotedFor.Unlock()
	}

	//rf.muForTerm.Lock()
	if followerChange.term > rf.Term {
		rf.Term = followerChange.term
	}
	//rf.muForTerm.Unlock()

	for i := 0; i < len(rf.peers); i++ {
		rf.muForNextIndex.Lock()
		rf.nextIndex[i] = 1
		rf.muForNextIndex.Unlock()
	}

	rf.persist()
	rf.followerChangeCompleted <- true
	if followerChange.reset {
		randNo := rand.New(rand.NewSource(time.Now().UnixNano()))
		timeout := randNo.Intn(electionTimeoutMs) + electionTimeoutMs
		rf.timeout.Reset(time.Duration(timeout) * time.Millisecond)
	}
	rf.assumeFollowerRole()
}

func (rf *Raft) AppendEntries(request AppendLogRequest, response *AppendLogReply) {
	rfTermGreaterThanCurrentTerm := false
	rf.muForTerm.Lock()
	if rf.Term > request.CurrentTerm {
		rfTermGreaterThanCurrentTerm = true
	}
	rf.muForTerm.Unlock()

	if rfTermGreaterThanCurrentTerm {
		rf.muForTerm.Lock()
		response.SuccessTerm = rf.Term
		response.Success = false
		rf.muForTerm.Unlock()
	} else {
		logLen := len(rf.Log)
		rf.checkLogEntryInPreviousLogTerm(request, response, logLen)
		rf.followerChange <- FollowerChange{request.CurrentTerm, request.LeaderId, true}
		<-rf.followerChangeCompleted
	}
}

func (rf *Raft) checkLogEntryInPreviousLogTerm(request AppendLogRequest, response *AppendLogReply, logLen int) {
	isLess := false
	mismatch := false

	if logLen < request.PrevLogIndex+1 {
		isLess = true
	}

	if !isLess && request.PrevLogIndex > 0 && (rf.Log[request.PrevLogIndex].Term != request.PrevLogTerm) {
		mismatch = true
	}

	if isLess || mismatch {
		rf.indexMisMatchOrLess(request, response, logLen, isLess, mismatch)
	} else {
		rf.existingEntryConflictsWithNewOne(request, response)
	}
}

func (rf *Raft) indexMisMatchOrLess(request AppendLogRequest, response *AppendLogReply, logLen int, isLess bool, mismatch bool) {
	response.SuccessTerm = request.CurrentTerm
	response.Success = false
	if isLess {
		response.Term = -1
		response.Index = logLen
	} else if mismatch {
		response.Term = rf.Log[request.PrevLogIndex].Term
		for i := 0; i < logLen; i++ {
			if rf.Log[i].Term == response.Term {
				response.Index = i
				break
			}
		}
	}
}

func (rf *Raft) existingEntryConflictsWithNewOne(request AppendLogRequest, response *AppendLogReply) {
	rf.muForLog.Lock()

	if len(rf.Log)-1 != request.PrevLogIndex {
		rf.Log = rf.Log[:request.PrevLogIndex+1]
	}

	rf.Log = append(rf.Log, request.Logs...)
	rf.muForCommitIndex.Lock()
	previousCommitIndex := rf.commitIndex
	if len(rf.Log)-1 > request.CommitIndex {
		rf.commitIndex = request.CommitIndex
	} else {
		rf.commitIndex = len(rf.Log) - 1
	}
	rf.muForCommitIndex.Unlock()

	rf.muForLog.Unlock()

	rf.persist()

	rf.muForLog.Lock()
	for i := previousCommitIndex + 1; i <= rf.commitIndex; i++ {
		rf.applyCh <- ApplyMsg{Index: i, Command: rf.Log[i].Command}
	}
	rf.muForLog.Unlock()

	response.SuccessTerm = request.CurrentTerm
	response.Success = true
}

func (rf *Raft) sendAppendEntries(server int, args AppendLogRequest, reply *AppendLogReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) synchronizeLogsWithAllPeers() {
	for peer := 0; peer < len(rf.peers); peer++ {
		if peer != rf.me {
			go rf.sendMessageToPeer(peer)
		}
	}
}

func (rf *Raft) sendMessageToPeer(peer int) {
	request, maxLogEntryIndex := rf.appendLogRequest(peer)
	if maxLogEntryIndex == -1 {
		return
	}

	var response AppendLogReply
	sendOk := rf.sendAppendEntries(peer, request, &response)
	rf.mu.Lock()
	rf.muForTerm.Lock()
	sameTerm := request.CurrentTerm == rf.Term
	rf.muForTerm.Unlock()
	rf.mu.Unlock()
	if sendOk && sameTerm {
		rf.logInConsistenceHandling(peer, response, maxLogEntryIndex)
	}

}

func (rf *Raft) appendLogRequest(peer int) (AppendLogRequest, int) {
	rf.mu.Lock()
	if rf.assumeRole != leader {
		rf.mu.Unlock()
		return AppendLogRequest{}, -1
	} else {
		rf.muForNextIndex.Lock()
		startIndex := rf.nextIndex[peer]
		rf.muForNextIndex.Unlock()
		endIndex := len(rf.Log)
		request := AppendLogRequest{}
		rf.muForTerm.Lock()
		request.CurrentTerm = rf.Term
		rf.muForTerm.Unlock()
		request.LeaderId = rf.me

		prevLogIndex := startIndex - 1
		if prevLogIndex < 0 {
			prevLogIndex = 0
		}
		request.PrevLogIndex = prevLogIndex
		request.PrevLogTerm = rf.Log[prevLogIndex].Term

		if startIndex != -1 && endIndex > 0 {
			request.Logs = rf.Log[startIndex:endIndex]
		}
		request.CommitIndex = rf.commitIndex
		rf.mu.Unlock()

		return request, endIndex
	}

}

func (rf *Raft) logInConsistenceHandling(serverIndex int, response AppendLogReply, maxLogEntryIndex int) {
	if response.Success {
		rf.muForNextIndex.Lock()
		rf.nextIndex[serverIndex] = maxLogEntryIndex
		rf.muForNextIndex.Unlock()

		rf.muForMatchIndex.Lock()
		rf.matchIndex[serverIndex] = maxLogEntryIndex - 1
		rf.muForMatchIndex.Unlock()
		rf.followerAppendEntries <- true
	} else {
		rf.muForTerm.Lock()

		if response.SuccessTerm > rf.Term {
			rf.followerChange <- FollowerChange{response.SuccessTerm, -1, false}
			<-rf.followerChangeCompleted
		} else if response.SuccessTerm == rf.Term {
			rf.mu.Lock()
			nextIndex := -1
			for j := 1; j < len(rf.Log)-1; j++ {
				if rf.Log[j].Term == response.SuccessTerm && rf.Log[j+1].Term != response.SuccessTerm {
					nextIndex = j + 1
					break
				}
			}

			rf.muForNextIndex.Lock()
			if response.Term == -1 || nextIndex == -1 {
				rf.nextIndex[serverIndex] = response.Index
			} else {
				rf.nextIndex[serverIndex] = nextIndex
			}
			rf.muForNextIndex.Unlock()
			rf.mu.Unlock()
		}

		rf.muForTerm.Unlock()
	}
}

func (rf *Raft) SetCommitIndex() {
	functionEnd := false
	majorityIndex := (len(rf.peers) - 1) / 2
	for {
		select {
		case <-rf.receivedQuit:
			{
				functionEnd = true
				break
			}
		case <-rf.followerAppendEntries:
			{
			}
		}
		rf.updateCommitIndex(majorityIndex)

		if functionEnd == true {
			break
		}
	}
}

func (rf *Raft) updateCommitIndex(index int) {
	rf.mu.Lock()
	onMajority := false
	commitOnAll := false

	previousIndex := rf.commitIndex
	rf.muForMatchIndex.Lock()
	matchIndex := append([]int{}, rf.matchIndex...)
	rf.muForMatchIndex.Unlock()
	sort.Ints(matchIndex)

	indexMajority := matchIndex[index]
	if (previousIndex < indexMajority) && (indexMajority < len(rf.Log)) && (rf.Log[indexMajority].Term == rf.Term) {
		onMajority = true
	}

	indexAll := matchIndex[0]
	if (previousIndex < indexAll) && (indexAll < len(rf.Log)) {
		commitOnAll = true
	}

	if onMajority {
		rf.commitIndex = indexMajority
		rf.persist()
		for i := previousIndex + 1; i <= rf.commitIndex; i++ {
			rf.applyCh <- ApplyMsg{Index: i, Command: rf.Log[i].Command}
		}

	} else if commitOnAll {
		rf.commitIndex = indexAll
		rf.persist()
		for i := previousIndex + 1; i <= rf.commitIndex; i++ {
			rf.applyCh <- ApplyMsg{Index: i, Command: rf.Log[i].Command}
		}

	}

	rf.mu.Unlock()
}
