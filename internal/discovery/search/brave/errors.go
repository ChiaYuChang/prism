package brave

import "errors"

var ErrRateLimited = errors.New("brave search: rate limited (429)")
