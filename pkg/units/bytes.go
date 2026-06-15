package units

import (
	"fmt"
	"strconv"
	"strings"
)

// Bytes is a human-readable byte size used by configuration.
type Bytes string

// ParseBytes parses byte sizes such as 512KB, 10MB, 1GiB, or plain bytes.
func ParseBytes(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}

	upper := strings.ToUpper(raw)
	units := []struct {
		suffix string
		mult   int64
	}{
		{"KIB", 1024},
		{"MIB", 1024 * 1024},
		{"GIB", 1024 * 1024 * 1024},
		{"KB", 1000},
		{"MB", 1000 * 1000},
		{"GB", 1000 * 1000 * 1000},
		{"B", 1},
	}

	mult := int64(1)
	number := upper
	for _, unit := range units {
		if strings.HasSuffix(upper, unit.suffix) {
			mult = unit.mult
			number = strings.TrimSpace(strings.TrimSuffix(upper, unit.suffix))
			break
		}
	}
	if number == "" {
		return 0, fmt.Errorf("invalid byte size %q", raw)
	}
	value, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte size %q: %w", raw, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("byte size must be >= 0")
	}
	return int64(value * float64(mult)), nil
}

// Int64 returns the parsed size in bytes.
func (b Bytes) Int64() (int64, error) {
	return ParseBytes(string(b))
}

func (b Bytes) String() string {
	return string(b)
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (b *Bytes) UnmarshalText(text []byte) error {
	_, err := ParseBytes(string(text))
	if err != nil {
		return err
	}
	*b = Bytes(strings.TrimSpace(string(text)))
	return nil
}
