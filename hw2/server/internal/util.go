package internal

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/metadata"
)

func getSender(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		hosts := md[":authority"]
		if len(hosts) > 0 {
			return hosts[0], nil
		} else {
			return "", fmt.Errorf("grpc request authority is bad")
		}
	} else {
		return "", fmt.Errorf("grpc request metadata is bad")
	}
}

func tick(timer *time.Timer, timeout time.Duration, callback func()) *time.Timer {
	if timer != nil {
		timer.Stop()
	}
	return time.AfterFunc(timeout, callback)
}
