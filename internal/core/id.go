package core

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var entropyMu sync.Mutex

// NewID returns a new ULID string.
func NewID() ID {
	entropyMu.Lock()
	defer entropyMu.Unlock()

	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader).String()
}

// Must panics if err is not nil.
func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// MergeErrors collects per-node errors into a single deterministic error.
func MergeErrors(errs map[NodeName]error) error {
	if len(errs) == 0 {
		return nil
	}

	nodes := make([]string, 0, len(errs))
	for node, err := range errs {
		if err == nil {
			continue
		}
		nodes = append(nodes, string(node))
	}
	if len(nodes) == 0 {
		return nil
	}

	sort.Strings(nodes)
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		parts = append(parts, fmt.Sprintf("%s: %v", node, errs[NodeName(node)]))
	}

	return fmt.Errorf("merge errors: %s", strings.Join(parts, "; "))
}
