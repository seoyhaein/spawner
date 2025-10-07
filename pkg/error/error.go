package error

import "errors"

var ErrNoMatch = errors.New("no matching frontdoor rule")
