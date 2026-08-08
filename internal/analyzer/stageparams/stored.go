package stageparams

import "encoding/json"

type Stored struct {
	Stage   string          `json:"stage"`
	Version int             `json:"version"`
	Body    json.RawMessage `json:"body"`
}
