package testutils

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"golang.org/x/net/idna"
)

// CharSet represents a set of characters for random generation.
type CharSet string

const (
	CharSetUpperCase    CharSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	CharSetLowerCase    CharSet = "abcdefghijklmnopqrstuvwxyz"
	CharSetNumbers      CharSet = "0123456789"
	CharSetAlphaNumeric CharSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

var (
	ErrInvalidLength    = errors.New("invalid length")
	ErrEmptyCharSet     = errors.New("empty character set")
	ErrInvalidDimension = errors.New("invalid dimension for vector generation")
	ErrInvalidRange     = errors.New("invalid range for vector generation")
)

// Runes returns the characters in the CharSet as a rune slice.
func (c CharSet) Runes() []rune {
	return []rune(string(c))
}

// RandomWord generates a random string of a specified length using the given CharSet.
func RandomWord(length int, charSet CharSet) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("%w: length must be > 0, got %d", ErrInvalidLength, length)
	}
	if len(charSet) == 0 {
		return "", ErrEmptyCharSet
	}

	runes := charSet.Runes()
	result := make([]rune, length)
	for i := range result {
		result[i] = runes[rand.IntN(len(runes))]
	}
	return string(result), nil
}

// RandomParagraph generates a string consisting of multiple random words.
func RandomParagraph(nWords, minLen, maxLen int, sep string, charSet CharSet) (string, error) {
	if nWords <= 0 {
		return "", ErrInvalidLength
	}

	delta := maxLen - minLen
	words := make([]string, nWords)
	for i := range words {
		wLen := minLen
		if delta > 0 {
			wLen += rand.IntN(delta)
		}
		word, err := RandomWord(wLen, charSet)
		if err != nil {
			return "", err
		}
		words[i] = word
	}
	return strings.Join(words, sep), nil
}

// RandomUrl generates a mock URL for testing discovery/scraping logic.
func RandomUrl(labels, segments int, domainSet, pathSet CharSet) (string, error) {
	if labels <= 0 {
		return "", ErrInvalidLength
	}

	rawDomain := make([]string, labels)
	for i := range rawDomain {
		word, _ := RandomWord(rand.IntN(8)+3, domainSet)
		rawDomain[i] = word
	}

	rawPath := make([]string, segments)
	for i := range rawPath {
		word, _ := RandomWord(rand.IntN(10)+2, pathSet)
		rawPath[i] = word
	}

	domain, _ := idna.ToASCII(strings.Join(rawDomain, "."))
	path := url.PathEscape(strings.Join(rawPath, "/"))
	return fmt.Sprintf("%s/%s", domain, path), nil
}

// RandomPGVector generates a random vector of a specified dimension.
// Extremely useful for testing Phase 4 semantic distance calculations.
func RandomPGVector(dim int, ub, lb float32) (pgvector.Vector, error) {
	if dim <= 0 {
		return pgvector.Vector{}, ErrInvalidDimension
	}

	delta := ub - lb
	if delta <= 0 {
		return pgvector.Vector{}, ErrInvalidRange
	}

	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rand.Float32()*delta + lb
	}
	return pgvector.NewVector(vec), nil
}

// RandomTime generates a random time between two given points.
func RandomTime(min, max time.Time) (time.Time, error) {
	if min.After(max) {
		return time.Time{}, ErrInvalidRange
	}
	delta := max.Sub(min)
	if delta <= 0 {
		return min, nil
	}
	return min.Add(time.Duration(rand.Int64N(int64(delta)))), nil
}

// MergeCharSets combines multiple CharSets into one, sorted and unique.
func MergeCharSets(sets ...CharSet) CharSet {
	m := make(map[rune]struct{})
	for _, s := range sets {
		for _, r := range s {
			m[r] = struct{}{}
		}
	}
	runes := make([]rune, 0, len(m))
	for r := range m {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	return CharSet(string(runes))
}
