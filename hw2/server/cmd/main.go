package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap"

	"tors.com/raft/server/internal"
)

func run() error {
	conf := flag.String("config", "config.yaml", "Path to the config file")
	flag.Parse()

	cfg, err := internal.LoadConfig(*conf)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	logger := zap.NewExample().Sugar()
	defer logger.Sync()

	app, err := internal.NewApp(logger, cfg)
	if err != nil {
		return fmt.Errorf("failed to create app: %v", err)
	}

	return app.Run(context.Background())
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "server failed: %v\n", err)
		os.Exit(1)
	}
}
