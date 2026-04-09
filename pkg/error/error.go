package error

import "errors"

var (
	ErrNoMatch         = errors.New("no matching frontdoor rule")
	ErrInvalidCommand  = errors.New("invalid command")
	ErrInvalidSpawnKey = errors.New("invalid spawn key")
	ErrSaturated       = errors.New("dispatcher saturated: max concurrent actors reached")
	ErrMailboxFull     = errors.New("mailbox full")
	ErrK8sUnavailable  = errors.New("k8s unavailable: cluster unreachable at startup")
)
