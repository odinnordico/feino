package testserver

import (
	"math"
	"strings"
	"unicode"
)

// vectorIndex holds precomputed term-frequency vectors for a corpus of Records.
// All methods are read-only after construction — safe for concurrent use.
type vectorIndex struct {
	records []Record
	vectors []map[string]float64 // parallel to records
}

// newVectorIndex precomputes TF vectors for every record in the corpus.
// Panics if records is empty — callers must guarantee a non-empty corpus.
func newVectorIndex(records []Record) *vectorIndex {
	if len(records) == 0 {
		panic("testserver: NewSimulatedServer requires at least one Record")
	}
	idx := &vectorIndex{records: records}
	for _, r := range records {
		idx.vectors = append(idx.vectors, termFreq(tokenize(r.Prompt)))
	}
	return idx
}

// findBestMatch returns the Record whose TF vector has the highest cosine
// similarity to query, along with the similarity score (0..1).
// Returns a zero Record and 0.0 if the index is somehow empty.
func (idx *vectorIndex) findBestMatch(query string) (best Record, score float64) {
	if len(idx.records) == 0 {
		return Record{}, 0
	}
	qVec := termFreq(tokenize(query))
	var bestScore float64
	var bestIdx int
	for i, vec := range idx.vectors {
		if score := cosineSimilarity(qVec, vec); score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return idx.records[bestIdx], bestScore
}

// tokenize lowercases s and splits it on non-alphanumeric runes, returning
// only tokens of length ≥ 2. Single-character tokens (articles, punctuation
// fragments) are filtered without a stop-word list.
func tokenize(s string) []string {
	lower := strings.ToLower(s)
	raw := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	result := raw[:0] // reuse backing array
	for _, tok := range raw {
		if len(tok) >= 2 {
			result = append(result, tok)
		}
	}
	return result
}

// termFreq counts raw occurrences of each token, returning an empty map for
// an empty token slice.
func termFreq(tokens []string) map[string]float64 {
	tf := make(map[string]float64, len(tokens))
	for _, tok := range tokens {
		tf[tok]++
	}
	return tf
}

// cosineSimilarity returns the cosine similarity (0..1) between two TF maps.
// Returns 0 when either map is empty or has zero magnitude.
func cosineSimilarity(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	// Iterate the smaller map for the dot product to minimise look-ups.
	small, large := a, b
	if len(a) > len(b) {
		small, large = b, a
	}
	var dot float64
	for k, v := range small {
		dot += v * large[k]
	}

	var magA, magB float64
	for _, v := range a {
		magA += v * v
	}
	for _, v := range b {
		magB += v * v
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
