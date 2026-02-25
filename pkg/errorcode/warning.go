package errorcode

import (
	"encoding/json"
	"fmt"
)

var ErrInvalidWarningLevel = fmt.Errorf("invalid warning level")

// WarningLevel defines the severity of a non-fatal warning.
type WarningLevel string

const (
	WarnInfo WarningLevel = "INFO"
	WarnLow  WarningLevel = "LOW"
	WarnHigh WarningLevel = "HIGH"
)

func (w *WarningLevel) Parse(data []byte) error {
	s := string(data)
	switch s {
	case "INFO":
		*w = WarnInfo
	case "LOW":
		*w = WarnLow
	case "HIGH":
		*w = WarnHigh
	default:
		return ErrInvalidWarningLevel
	}
	return nil
}

func (w *WarningLevel) UnmarshalJSON(data []byte) error {
	return w.Parse(data)
}

func (w WarningLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(w))
}

// Warning records non-fatal information that does not interrupt the pipeline flow.
type Warning struct {
	Level   WarningLevel `json:"level"`
	Message string       `json:"message"`
}

// String implements the Stringer interface for formatted warning output.
func (w Warning) String() string {
	return fmt.Sprintf("[%s] %s", w.Level, w.Message)
}
