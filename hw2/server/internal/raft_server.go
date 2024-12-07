package internal

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"

	proto "tors.com/raft/server/proto/raft"
)

type raftServer struct {
	proto.UnimplementedRaftServer

	raftClients      map[string]*raftClient
	httpServers      []string
	service          *dbService
	leaderHost       string
	log              []*proto.LogEntry
	curTerm          int64
	commitedIndex    int64
	electionTimeout  time.Duration
	electionTimer    *time.Timer
	curVote          string
	heartbeatTimeout time.Duration
	heartbeatTimer   *time.Timer
	lastIdxs         map[string]int64
	state            NodeState
	mu               sync.Mutex

	logger *zap.SugaredLogger
}

func (raft *raftServer) AppendEntries(ctx context.Context, req *proto.AppendEntriesRequest) (*proto.AppendEntriesResponse, error) {
	sender, err := getSender(ctx)
	if err != nil {
		raft.logger.Errorw(
			"AppendEntriesError",
			"error", err,
		)
		return nil, err
	}

	raft.logger.Infow(
		"AppendEntriesStart",
		"term", req.Term,
		"sender", sender,
	)

	raft.mu.Lock()
	if raft.curTerm > req.Term {
		raft.logger.Warnw(
			"AppendEntriesWarn",
			"warn", "term of host is greater than request term",
		)
		return &proto.AppendEntriesResponse{Success: false}, nil
	}

	raft.updateState(req, sender)

	if !raft.goodEntry(req) {
		raft.logger.Warnw(
			"AppendEntriesWarn",
			"warn", "wrong prev entry term or index, send previous one",
		)
		return &proto.AppendEntriesResponse{Success: false}, nil
	}

	if req.LastEntryIndex+1 < int64(len(raft.log)) {
		raft.log = raft.log[:req.LastEntryIndex+1]
	}

	for _, entry := range req.Entries {
		raft.log = append(raft.log, entry)
	}
	raft.mu.Unlock()

	for i := raft.commitedIndex + 1; i <= req.LeaderCommitedIndex; i++ {
		raft.logger.Infow(
			"CommitEntry",
			"index", i,
			"type", raft.log[i].Type,
			"key", raft.log[i].Key,
			"value", raft.log[i].Value,
		)
		raft.service.eval(raft.log[i].Type, raft.log[i].Key, raft.log[i].Value)
	}
	raft.mu.Lock()
	raft.commitedIndex = req.LeaderCommitedIndex
	raft.mu.Unlock()

	raft.logger.Infow(
		"AppendEntriesEnd",
	)

	return &proto.AppendEntriesResponse{Success: true}, nil
}

func (raft *raftServer) RequestVote(ctx context.Context, req *proto.RequestVoteRequest) (*proto.RequestVoteResponse, error) {
	sender, err := getSender(ctx)
	if err != nil {
		raft.logger.Errorw(
			"VoteRequestedError",
			"error", err,
		)
		return nil, err
	}

	raft.logger.Infow(
		"VoteRequested",
		"sender", sender,
		"term", req.Term,
	)

	raft.mu.Lock()
	defer raft.mu.Unlock()
	prevIdx := int64(len(raft.log) - 1)
	if (raft.curVote == "" || req.Term > raft.curTerm || raft.curVote == sender) && req.LastEntryIndex >= prevIdx && (prevIdx < 0 || req.LastEntryTerm >= raft.log[prevIdx].Term) {
		raft.logger.Infow(
			"VoteGranted",
			"sender", sender,
			"term", req.Term,
		)

		raft.curTerm = req.Term
		raft.curVote = sender
		return &proto.RequestVoteResponse{VoteGranted: true}, nil
	}

	raft.logger.Infow(
		"VoteNotGranted",
		"sender", sender,
		"term", req.Term,
	)
	return &proto.RequestVoteResponse{VoteGranted: false}, nil
}

func newRaftServer(logger *zap.SugaredLogger, config *Config) (*raftServer, error) {
	raftClients := make(map[string]*raftClient)
	var httpServers []string
	for i, host := range config.GrpcServers {
		client, err := newRaftClient(logger, host)
		if err != nil {
			return nil, err
		}
		raftClients[host] = client
		httpServers = append(httpServers, config.HttpServers[i])
	}

	server := raftServer{
		raftClients:      raftClients,
		httpServers:      httpServers,
		service:          newDBService(),
		leaderHost:       "",
		curTerm:          0,
		commitedIndex:    -1,
		logger:           logger,
		electionTimeout:  time.Second * time.Duration(config.MinElectionTimeout+config.Id),
		heartbeatTimeout: time.Second * time.Duration(config.HeartbeatTimeout),
		lastIdxs:         make(map[string]int64),
		state:            FOLLOWER,
	}

	server.electionTimer = tick(nil, server.electionTimeout, server.startElection)

	return &server, nil
}

func (raft *raftServer) replicate(ctx context.Context, entry *proto.LogEntry) uint64 {
	ackCh := make(chan bool, len(raft.raftClients))
	for host, client := range raft.raftClients {
		go func(host string, client *raftClient) {
			raft.mu.Lock()
			raft.logger.Infow(
				"Replicate",
				"host", host,
				"term", raft.curTerm,
			)
			lastEntryIndex := int64(len(raft.log) - 2)
			var lastEntryTerm int64 = 0
			if lastEntryIndex >= 0 {
				lastEntryTerm = raft.log[lastEntryIndex].Term
			}

			req := proto.AppendEntriesRequest{
				Term:           raft.curTerm,
				LastEntryIndex: lastEntryIndex,
				LastEntryTerm:  lastEntryTerm,
				Entries:        []*proto.LogEntry{entry},
			}
			raft.mu.Unlock()

			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()

			resp, err := (*client).AppendEntries(ctx, &req)
			raft.mu.Lock()
			defer raft.mu.Unlock()
			if err == nil && resp.Success {
				ackCh <- true
				raft.lastIdxs[host]++
			} else {
				ackCh <- false
				if err != nil {
					raft.logger.Errorw(
						"ReplicateError",
						"host", host,
						"error", err,
					)
				}
			}
		}(host, client)
	}

	var ackCount uint64 = 1
	for i := 0; i < len(raft.raftClients); i++ {
		if <-ackCh {
			ackCount++
		}
	}
	return ackCount
}

func (raft *raftServer) startElection() {
	raft.logger.Info("StartElection")

	raft.mu.Lock()
	raft.curTerm++
	raft.curVote = "this"
	raft.state = CANDIDATE
	raft.mu.Unlock()
	var votesGranted uint64 = 1

	for host, client := range raft.raftClients {
		go raft.requestVote(host, client, &votesGranted)
	}

	raft.electionTimer = tick(raft.electionTimer, raft.electionTimeout, raft.startElection)
}

func (raft *raftServer) requestVote(host string, client *raftClient, votesGranted *uint64) {
	raft.logger.Infow(
		"RequestVote",
		"host", host,
	)

	var req proto.RequestVoteRequest
	raft.mu.Lock()
	if len(raft.log) > 0 {
		req = proto.RequestVoteRequest{
			Term:           raft.curTerm,
			LastEntryIndex: int64(len(raft.log) - 1),
			LastEntryTerm:  raft.log[len(raft.log)-1].Term,
		}
	} else {
		req = proto.RequestVoteRequest{
			Term:           raft.curTerm,
			LastEntryIndex: -1,
			LastEntryTerm:  0,
		}
	}
	raft.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := (*client).RequestVote(ctx, &req)
	if err != nil {
		raft.logger.Errorw(
			"RequestVoteError",
			"host", host,
			"error", err,
		)
		return
	}

	if resp.VoteGranted {
		raft.mu.Lock()
		defer raft.mu.Unlock()
		(*votesGranted)++
		raft.logger.Infow(
			"VoteGranted",
			"host", host,
		)
		if (*votesGranted) > uint64((len(raft.raftClients)+1)/2) && raft.state == CANDIDATE {
			raft.becomeLeader()
		}
	}
}

func (raft *raftServer) becomeLeader() {
	raft.logger.Info("BecomeLeader")

	raft.leaderHost = "this"
	raft.state = LEADER
	for host := range raft.raftClients {
		raft.lastIdxs[host] = int64(len(raft.log))
	}

	raft.heartbeatTimer = tick(nil, raft.heartbeatTimeout, raft.sendHeartbeats)
	if raft.electionTimer != nil {
		raft.electionTimer.Stop()
	}
}

func (raft *raftServer) sendHeartbeats() {
	raft.mu.Lock()
	if raft.state != LEADER {
		raft.mu.Unlock()
		return
	}
	raft.mu.Unlock()

	for host, client := range raft.raftClients {
		go raft.sendHeartbeat(host, client)
	}

	raft.heartbeatTimer = tick(raft.heartbeatTimer, raft.heartbeatTimeout, raft.sendHeartbeats)
}

func (raft *raftServer) sendHeartbeat(host string, client *raftClient) {
	for {
		raft.logger.Infow(
			"HeartbeatTry",
			"host", host,
		)
		raft.mu.Lock()
		lastIdx := raft.lastIdxs[host]
		entries := raft.log[lastIdx:]
		lastEntryIndex := lastIdx - 1
		lastEntryTerm := int64(0)
		if lastEntryIndex >= 0 {
			lastEntryTerm = raft.log[lastEntryTerm].Term
		}

		req := proto.AppendEntriesRequest{
			Term:                raft.curTerm,
			LastEntryIndex:      lastEntryIndex,
			LastEntryTerm:       lastEntryTerm,
			LeaderCommitedIndex: raft.commitedIndex,
			Entries:             entries,
		}
		raft.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		resp, err := (*client).AppendEntries(ctx, &req)
		if err != nil {
			raft.logger.Errorw(
				"HeartbeatError",
				"host", host,
				"error", err,
			)
			return
		}

		raft.mu.Lock()
		if resp.Success {
			raft.lastIdxs[host] += int64(len(entries))
			raft.mu.Unlock()
			break
		} else {
			raft.lastIdxs[host]--
			raft.mu.Unlock()
		}
	}
}

func (raft *raftServer) updateState(req *proto.AppendEntriesRequest, sender string) {
	raft.curTerm = req.Term
	raft.leaderHost = sender
	raft.state = FOLLOWER
	raft.curVote = ""
	raft.electionTimer = tick(raft.electionTimer, raft.electionTimeout, raft.startElection)
}

func (raft *raftServer) goodEntry(req *proto.AppendEntriesRequest) bool {
	return req.LastEntryIndex < 0 || req.LastEntryIndex < int64(len(raft.log)) && req.LastEntryTerm == raft.log[req.LastEntryIndex].Term
}

func (raft *raftServer) processNewEntry(ctx context.Context, entry *proto.LogEntry) error {
	raft.mu.Lock()
	raft.log = append(raft.log, entry)
	raft.mu.Unlock()
	ackCount := raft.replicate(ctx, entry)
	if ackCount > uint64(len(raft.raftClients)+1)/2 {
		raft.logger.Infow(
			"CommitEntry",
			"index", len(raft.log)-1,
			"type", entry.Type,
			"key", entry.Key,
			"value", entry.Value,
		)
		raft.service.eval(entry.Type, entry.Key, entry.Value)
		raft.mu.Lock()
		raft.commitedIndex++
		raft.mu.Unlock()
		return nil
	}
	return fmt.Errorf("replication error")
}

func (raft *raftServer) create(ctx context.Context, key string, value string) error {
	raft.mu.Lock()
	if raft.state != LEADER {
		return fmt.Errorf("this pod is not a leader")
	}

	newEntry := &proto.LogEntry{
		Type:  "create",
		Index: int64(len(raft.log) - 1),
		Term:  raft.curTerm,
		Key:   key,
		Value: &value,
	}
	raft.mu.Unlock()

	return raft.processNewEntry(ctx, newEntry)
}

func (raft *raftServer) update(ctx context.Context, key string, value string) error {
	raft.mu.Lock()
	if raft.state != LEADER {
		return fmt.Errorf("this pod is not a leader")
	}

	newEntry := &proto.LogEntry{
		Type:  "update",
		Index: int64(len(raft.log) - 1),
		Term:  raft.curTerm,
		Key:   key,
		Value: &value,
	}
	raft.mu.Unlock()

	return raft.processNewEntry(ctx, newEntry)
}

func (raft *raftServer) delete(ctx context.Context, key string) error {
	raft.mu.Lock()
	if raft.state != LEADER {
		return fmt.Errorf("this pod is not a leader")
	}

	newEntry := &proto.LogEntry{
		Type:  "delete",
		Index: int64(len(raft.log) - 1),
		Term:  raft.curTerm,
		Key:   key,
		Value: nil,
	}
	raft.mu.Unlock()

	return raft.processNewEntry(ctx, newEntry)
}

func (raft *raftServer) get(_ context.Context, key string) (*string, *bool, *string) {
	raft.mu.Lock()
	if raft.state == LEADER {
		randKey := rand.Intn(len(raft.httpServers))
		raft.mu.Unlock()
		server := raft.httpServers[randKey]
		return nil, nil, &server
	}
	raft.mu.Unlock()

	val, ok := raft.service.get(key)
	return &val, &ok, nil
}
