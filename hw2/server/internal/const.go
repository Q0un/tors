package internal

type NodeState int64

const (
	LEADER NodeState = iota
	CANDIDATE
	FOLLOWER
)
