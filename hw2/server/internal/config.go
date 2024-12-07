package internal

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Id                 uint64   `yaml:"id"`
	HttpAddress        string   `yaml:"http_address"`
	GrpcAddress        string   `yaml:"grpc_address"`
	HttpServers        []string `yaml:"http_servers"`
	GrpcServers        []string `yaml:"grpc_servers"`
	MinElectionTimeout uint64   `yaml:"min_election_timeout"`
	HeartbeatTimeout   uint64   `yaml:"heartbeat_timeout"`
}

func LoadConfig(path string) (*Config, error) {
	r, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	conf := &Config{}
	err = yaml.Unmarshal(r, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return conf, nil
}
