package error

import "errors"

var (
	ErrNoMatch     = errors.New("no matching frontdoor rule")
	ErrSaturated   = errors.New("dispatcher saturated: max concurrent actors reached")
	ErrMailboxFull = errors.New("mailbox full")
)
