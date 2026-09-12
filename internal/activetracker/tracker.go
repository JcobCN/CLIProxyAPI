package activetracker

import (
	"sort"
	"sync"
	"time"
)

// GinContextKey is the Gin context key holding the *ActiveRequest entry for
// the current in-flight request, so downstream executors can enrich it.
const GinContextKey = "activeTrackerEntry"

type ActiveRequest struct {
	APIKey    string    `json:"api_key"`
	Model     string    `json:"model"`
	Kind      string    `json:"kind"`
	RequestID string    `json:"request_id,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

type Tracker struct {
	mu       sync.Mutex
	requests map[*ActiveRequest]struct{}
}

// Default is the process-wide active request tracker shared by the proxy auth
// middleware and the management endpoint.
var Default = New()

func New() *Tracker {
	return &Tracker{requests: make(map[*ActiveRequest]struct{})}
}

// Enter registers an active request and returns the entry (which may be
// enriched later via SetModel) plus a cleanup func that must be deferred by
// the caller for the request lifetime. Each request is keyed by a unique
// pointer so concurrent requests never collide.
func (t *Tracker) Enter(apiKey, model, kind, requestID string) (*ActiveRequest, func()) {
	if t == nil {
		return nil, func() {}
	}
	req := &ActiveRequest{APIKey: apiKey, Model: model, Kind: kind, RequestID: requestID, StartedAt: time.Now()}
	t.mu.Lock()
	t.requests[req] = struct{}{}
	t.mu.Unlock()
	return req, func() {
		t.mu.Lock()
		delete(t.requests, req)
		t.mu.Unlock()
	}
}

// SetModel fills in the model of an active request entry once it is known.
// It is a noop for entries that are missing or already carry a model.
func (t *Tracker) SetModel(entry *ActiveRequest, model string) {
	if t == nil || entry == nil || model == "" {
		return
	}
	t.mu.Lock()
	if _, ok := t.requests[entry]; ok && entry.Model == "" {
		entry.Model = model
	}
	t.mu.Unlock()
}

// Snapshot returns a copy of all currently active requests ordered by start time.
func (t *Tracker) Snapshot() []ActiveRequest {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]ActiveRequest, 0, len(t.requests))
	for req := range t.requests {
		result = append(result, *req)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartedAt.Before(result[j].StartedAt) })
	return result
}
