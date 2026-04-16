package utils

func MergeMap[S comparable, T any](m0, m1 map[S]T) map[S]T {
	merged := make(map[S]T, len(m0)+len(m1))
	for k, v := range m0 {
		merged[k] = v
	}
	for k, v := range m1 {
		merged[k] = v
	}
	return merged
}
