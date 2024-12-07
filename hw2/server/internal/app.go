package internal

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	proto "tors.com/raft/server/proto/raft"
)

type App struct {
	logger *zap.SugaredLogger
	router chi.Router
	config *Config

	api  *apiStruct
	raft *raftServer
}

func NewApp(logger *zap.SugaredLogger, config *Config) (*App, error) {
	raft, err := newRaftServer(logger, config)

	app := &App{
		logger: logger,
		config: config,
		api: &apiStruct{
			raft: raft,
		},
		raft: raft,
	}

	err = app.SetupRouter()
	if err != nil {
		return nil, err
	}

	return app, nil
}

func (app *App) SetupRouter() error {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(middleware.URLFormat)
	router.Use(middleware.Compress(5))
	router.Use(middleware.Logger)

	app.api.Mount(router)

	app.router = router
	return nil
}

func (app *App) Run(ctx context.Context) error {
	app.logger.Info("Starting app")

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return app.RunHTTPServer(ctx)
	})

	g.Go(func() error {
		return app.RunGRPCServer(ctx)
	})

	return g.Wait()
}

func (app *App) RunHTTPServer(ctx context.Context) error {
	app.logger.Infow(
		"Starting http server",
		"address", app.config.HttpAddress,
	)
	server := &http.Server{Addr: app.config.HttpAddress, Handler: app.router}
	return server.ListenAndServe()
}

func (app *App) RunGRPCServer(ctx context.Context) error {
	app.logger.Infow(
		"Starting grpc server at",
		"address", app.config.GrpcAddress,
	)
	server := grpc.NewServer()
	proto.RegisterRaftServer(server, app.raft)

	listen, err := net.Listen("tcp", app.config.GrpcAddress)
	if err != nil {
		return fmt.Errorf("failed to listen grpc server socket: %v", err)
	}

	if err = server.Serve(listen); err != nil {
		return fmt.Errorf("grpc server failed: %v", err)
	}

	return nil
}
