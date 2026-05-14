// Package debug is the lightweight in-memory event log surfaced by the TUI's
// /debug panel. Sites in the harness call Record/Recordf to emit events; the
// UI registers a Sink to be notified when new events arrive. All public
// functions are goroutine-safe.
//
// When Enabled() is false, Record short-circuits — production overhead is
// roughly one mutex lock and one bool check.
package debug

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Level classifies an event's severity. Used by the UI to color-code lines.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Event is one entry in the debug log. Message is the one-line summary
// shown in the panel; Payload is the optional long-form content (request
// JSON, full tool output, before/after transcripts) inspected via
// /debug show <id>.
//
// CorrelatedID links a response back to the request that produced it. The
// request event leaves it zero; the response sets it to the request's ID.
// Pair() walks the link in either direction.
type Event struct {
	ID           uint64
	CorrelatedID uint64 // 0 when no pair; otherwise the matching event's ID
	Time         time.Time
	Source       string // "provider", "tool", "compact", "subagent", …
	Level        Level
	Message      string
	Payload      string // optional; empty when there's nothing extra to inspect
}

const (
	ringCapacity      = 500
	maxPayloadBytes   = 100 * 1024 // per-event cap to keep memory bounded
	truncationMarker  = "\n…[truncated]"
)

var (
	mu      sync.Mutex
	events  = make([]Event, 0, ringCapacity)
	enabled bool
	sink    func(Event)
	nextID  atomic.Uint64
)

// Enabled reports whether recording is currently on.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}

// SetEnabled flips recording on or off. The UI calls this from the /debug
// slash command. Disabling does not clear the ring — old events stick around
// until Clear() or the harness exits.
func SetEnabled(b bool) {
	mu.Lock()
	enabled = b
	mu.Unlock()
}

// SetSink registers a callback fired on each Record. The TUI uses it to nudge
// a re-render of the debug panel. Pass nil to unregister.
func SetSink(f func(Event)) {
	mu.Lock()
	sink = f
	mu.Unlock()
}

// Record appends a payload-less event and returns its ID (0 if disabled).
func Record(source string, level Level, message string) uint64 {
	return record(0, source, level, message, "")
}

// Recordf is the printf variant of Record.
func Recordf(source string, level Level, format string, args ...any) uint64 {
	return record(0, source, level, fmt.Sprintf(format, args...), "")
}

// Recordp appends an event with an attached payload (full request JSON,
// tool output, before/after transcript) and returns its ID. Payload is
// truncated to a fixed per-event cap so a chatty conversation can't blow
// up memory.
func Recordp(source string, level Level, message, payload string) uint64 {
	return record(0, source, level, message, truncatePayload(payload))
}

// Recordpc is Recordp with a correlation back to a previously-recorded
// event. The new event's CorrelatedID is set to startID; Pair() can then
// walk the link in either direction from the UI.
func Recordpc(startID uint64, source string, level Level, message, payload string) uint64 {
	return record(startID, source, level, message, truncatePayload(payload))
}

// Recordfc is Recordf with a correlation back to a previously-recorded
// event. Used for the error-path response that pairs with an in-flight
// request.
func Recordfc(startID uint64, source string, level Level, format string, args ...any) uint64 {
	return record(startID, source, level, fmt.Sprintf(format, args...), "")
}

// record is the shared implementation. The sink (if any) is called outside
// the lock to avoid re-entrancy from sinks that take their own lock.
// Returns the new event's ID, or 0 if recording is disabled.
func record(correlatedID uint64, source string, level Level, message, payload string) uint64 {
	mu.Lock()
	if !enabled {
		mu.Unlock()
		return 0
	}
	if len(events) >= ringCapacity {
		// Drop the oldest. A real ring would use a head index; for ~500
		// events the slice shift is fine and keeps Snapshot trivial.
		events = events[1:]
	}
	e := Event{
		ID:           nextID.Add(1),
		CorrelatedID: correlatedID,
		Time:         time.Now(),
		Source:       source,
		Level:        level,
		Message:      message,
		Payload:      payload,
	}
	events = append(events, e)
	cb := sink
	mu.Unlock()
	if cb != nil {
		cb(e)
	}
	return e.ID
}

// Pair returns the event linked to id, in either direction. If the given
// event has a CorrelatedID, that's the request it's paired with. Otherwise
// Pair walks the ring looking for an event whose CorrelatedID equals id —
// that would be its response. Returns (zero, false) when no link exists or
// the paired event has aged out of the ring.
func Pair(id uint64) (Event, bool) {
	mu.Lock()
	defer mu.Unlock()
	var target Event
	var have bool
	for _, e := range events {
		if e.ID == id {
			target, have = e, true
			break
		}
	}
	if !have {
		return Event{}, false
	}
	if target.CorrelatedID != 0 {
		// Response → walk back to the request.
		for _, e := range events {
			if e.ID == target.CorrelatedID {
				return e, true
			}
		}
		return Event{}, false
	}
	// Request → walk forward to find a response that points back.
	for _, e := range events {
		if e.CorrelatedID == target.ID {
			return e, true
		}
	}
	return Event{}, false
}

// FindByID returns the event with the given monotonic ID, if it's still in
// the ring. Used by /debug show to look up a specific event.
func FindByID(id uint64) (Event, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, e := range events {
		if e.ID == id {
			return e, true
		}
	}
	return Event{}, false
}

// Latest returns the most recent event that has a non-empty payload, or
// (zero, false) if the ring is empty or no event has a payload yet.
// Used by /debug show with no argument.
func Latest() (Event, bool) {
	mu.Lock()
	defer mu.Unlock()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Payload != "" {
			return events[i], true
		}
	}
	if len(events) == 0 {
		return Event{}, false
	}
	return events[len(events)-1], true
}

func truncatePayload(p string) string {
	if len(p) <= maxPayloadBytes {
		return p
	}
	return p[:maxPayloadBytes] + truncationMarker
}

// Snapshot returns a copy of the current ring, oldest first.
func Snapshot() []Event {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Event, len(events))
	copy(out, events)
	return out
}

// Clear empties the ring. Called from /debug clear.
func Clear() {
	mu.Lock()
	events = events[:0]
	mu.Unlock()
}
