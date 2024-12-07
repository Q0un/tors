package internal

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	myproto "tors.com/raft/server/proto/api"
)

type apiStruct struct {
	myproto.UnimplementedHttpServer
	raft *raftServer
}

// CreateItem implements api.HttpServer
func (a *apiStruct) CreateItem(ctx context.Context, req *myproto.CreateItemRequest) (*myproto.Empty, error) {
	err := a.raft.create(ctx, req.Key, req.Value)
	if err != nil {
		return nil, err
	}
	return &myproto.Empty{}, nil
}

// UpdateItem implements api.HttpServer
func (a *apiStruct) UpdateItem(ctx context.Context, req *myproto.UpdateItemRequest) (*myproto.Empty, error) {
	err := a.raft.update(ctx, req.Key, req.Value)
	if err != nil {
		return nil, err
	}
	return &myproto.Empty{}, nil
}

// DeleteItem implements api.HttpServer
func (a *apiStruct) DeleteItem(ctx context.Context, req *myproto.DeleteItemRequest) (*myproto.Empty, error) {
	err := a.raft.delete(ctx, req.Key)
	if err != nil {
		return nil, err
	}
	return &myproto.Empty{}, nil
}

// GetItem implements api.HttpServer
func (a *apiStruct) GetItem(ctx context.Context, req *myproto.GetItemRequest) (*myproto.GetItemResponse, error) {
	val, ok, redir := a.raft.get(ctx, req.Key)
	if redir == nil {
		return &myproto.GetItemResponse{
			Value: val,
			Found: *ok,
		}, nil
	}

	md := metadata.Pairs("Location", ("http://localhost" + *redir + "/api/item?key=" + req.Key))
	grpc.SendHeader(ctx, md)

	return &myproto.GetItemResponse{}, nil
}

func responseHeaderMatcher(ctx context.Context, w http.ResponseWriter, resp proto.Message) error {
	headers := w.Header()
	if location, ok := headers["Grpc-Metadata-Location"]; ok {
		w.Header().Set("Location", location[0])
		w.WriteHeader(http.StatusFound)
	}

	return nil
}

func (a *apiStruct) Mount(router chi.Router) {
	mux := runtime.NewServeMux(
		runtime.WithForwardResponseOption(responseHeaderMatcher),
	)
	router.Mount("/api", mux)
	_ = myproto.RegisterHttpHandlerServer(context.Background(), mux, a)
}

var _ myproto.HttpServer = (*apiStruct)(nil)
