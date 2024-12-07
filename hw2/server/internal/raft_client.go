package internal

import (
	"context"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	proto "tors.com/raft/server/proto/raft"
)

type raftClient struct {
	conn   *grpc.ClientConn
	client proto.RaftClient
}

func newRaftClient(logger *zap.SugaredLogger, host string) (*raftClient, error) {
	logger.Infow(
		"Connecting to raft",
		"host", host,
	)
	cc, err := grpc.NewClient(host, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	return &raftClient{
		conn:   cc,
		client: proto.NewRaftClient(cc),
	}, nil
}

func (client *raftClient) AppendEntries(ctx context.Context, req *proto.AppendEntriesRequest) (*proto.AppendEntriesResponse, error) {
	return client.client.AppendEntries(ctx, req)
}

func (client *raftClient) RequestVote(ctx context.Context, req *proto.RequestVoteRequest) (*proto.RequestVoteResponse, error) {
	return client.client.RequestVote(ctx, req)
}

func (client *raftClient) Close() {
	client.conn.Close()
}
