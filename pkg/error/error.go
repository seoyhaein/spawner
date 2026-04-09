package error

import "errors"

var (
	ErrNoMatch            = errors.New("no matching frontdoor rule")
	ErrInvalidCommand     = errors.New("invalid command")
	ErrInvalidSpawnKey    = errors.New("invalid spawn key")
	ErrSaturated          = errors.New("dispatcher saturated: max concurrent actors reached")
	ErrMailboxFull        = errors.New("mailbox full")
	ErrBackendUnavailable = errors.New("backend unavailable: executor unreachable at startup")
)
