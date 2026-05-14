package debug_test

import (
	"testing"

	"github.com/betta-tech/byo-coding-agent/internal/debug"
)

// All tests in this file share the package's global state (the ring, the
// nextID counter, the sink). They run sequentially (no t.Parallel()) and
// reset state explicitly at the top of each test via Clear() + a known
// SetEnabled value.

func TestRecordReturnsZeroWhenDisabled(t *testing.T) {
	debug.SetEnabled(false)
	debug.Clear()

	id := debug.Record("src", debug.LevelInfo, "msg")
	if id != 0 {
		t.Errorf("expected ID 0 when recording disabled, got %d", id)
	}
	if got := len(debug.Snapshot()); got != 0 {
		t.Errorf("snapshot has %d events when disabled, want 0", got)
	}
}

func TestRecordAssignsMonotonicIDs(t *testing.T) {
	debug.SetEnabled(true)
	t.Cleanup(func() { debug.SetEnabled(false) })
	debug.Clear()

	id1 := debug.Record("src", debug.LevelInfo, "a")
	id2 := debug.Record("src", debug.LevelInfo, "b")
	id3 := debug.Record("src", debug.LevelInfo, "c")

	if id1 == 0 {
		t.Error("Record returned 0 while enabled")
	}
	if id2 != id1+1 || id3 != id2+1 {
		t.Errorf("IDs not sequential: %d, %d, %d", id1, id2, id3)
	}
}

func TestSnapshotReturnsCopy(t *testing.T) {
	debug.SetEnabled(true)
	t.Cleanup(func() { debug.SetEnabled(false) })
	debug.Clear()

	debug.Record("src", debug.LevelInfo, "first")
	snap := debug.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}

	// Mutating the snapshot must not affect future snapshots.
	snap[0].Message = "TAMPERED"
	again := debug.Snapshot()
	if again[0].Message == "TAMPERED" {
		t.Error("snapshot is a live reference, not a copy")
	}
}

// The ring drops the oldest event once it reaches capacity. We push more
// than the ring can hold and verify the front is gone, the back is intact,
// and FindByID can no longer locate the dropped events.
func TestRingDropsOldestPastCapacity(t *testing.T) {
	debug.SetEnabled(true)
	t.Cleanup(func() { debug.SetEnabled(false) })
	debug.Clear()

	const N = 600 // ring capacity is 500
	var firstID, lastID uint64
	for i := 0; i < N; i++ {
		id := debug.Recordf("src", debug.LevelInfo, "msg %d", i)
		if i == 0 {
			firstID = id
		}
		lastID = id
	}

	snap := debug.Snapshot()
	if len(snap) > N {
		t.Errorf("snapshot len = %d, expected <= ring capacity (%d)", len(snap), N)
	}
	if len(snap) == N {
		t.Error("ring didn't drop any events — capacity not enforced")
	}

	if snap[0].ID == firstID {
		t.Error("oldest event should have been dropped from the ring")
	}
	if snap[len(snap)-1].ID != lastID {
		t.Errorf("newest event missing; last ID is %d, want %d", snap[len(snap)-1].ID, lastID)
	}

	if _, ok := debug.FindByID(firstID); ok {
		t.Error("FindByID returned a dropped event")
	}
	if _, ok := debug.FindByID(lastID); !ok {
		t.Error("FindByID can't find the most recent event")
	}
}

// Pair walks both directions: a request emits → response correlates back.
// Calling Pair on either ID returns the other.
func TestPairWalksBothDirections(t *testing.T) {
	debug.SetEnabled(true)
	t.Cleanup(func() { debug.SetEnabled(false) })
	debug.Clear()

	reqID := debug.Recordp("provider", debug.LevelInfo, "→ Send", `{"req":1}`)
	respID := debug.Recordpc(reqID, "provider", debug.LevelInfo, "← Send", `{"resp":1}`)

	p, ok := debug.Pair(reqID)
	if !ok {
		t.Fatal("Pair(req) returned not-ok")
	}
	if p.ID != respID {
		t.Errorf("Pair(req) = #%d, want #%d (response)", p.ID, respID)
	}

	p, ok = debug.Pair(respID)
	if !ok {
		t.Fatal("Pair(resp) returned not-ok")
	}
	if p.ID != reqID {
		t.Errorf("Pair(resp) = #%d, want #%d (request)", p.ID, reqID)
	}
}

// Unpaired events return (zero, false). The lookup must not match itself.
func TestPairReturnsFalseForUnpairedEvent(t *testing.T) {
	debug.SetEnabled(true)
	t.Cleanup(func() { debug.SetEnabled(false) })
	debug.Clear()

	id := debug.Record("misc", debug.LevelInfo, "alone")

	if _, ok := debug.Pair(id); ok {
		t.Error("Pair on an unpaired event returned ok")
	}
}

// The sink is called once per Record while enabled, in order, with the
// fully-constructed Event.
func TestSinkReceivesEventsInOrder(t *testing.T) {
	debug.SetEnabled(true)
	t.Cleanup(func() {
		debug.SetEnabled(false)
		debug.SetSink(nil)
	})
	debug.Clear()

	var got []debug.Event
	debug.SetSink(func(e debug.Event) { got = append(got, e) })

	debug.Record("a", debug.LevelInfo, "first")
	debug.Record("b", debug.LevelWarn, "second")

	if len(got) != 2 {
		t.Fatalf("sink got %d events, want 2", len(got))
	}
	if got[0].Message != "first" || got[1].Message != "second" {
		t.Errorf("sink events out of order: %+v", got)
	}
	if got[0].Source != "a" || got[1].Source != "b" {
		t.Errorf("sink dropped Source field: %+v", got)
	}
}

// Latest walks back from the newest event to find one with a non-empty
// payload, skipping payload-less events along the way.
func TestLatestSkipsPayloadlessEvents(t *testing.T) {
	debug.SetEnabled(true)
	t.Cleanup(func() { debug.SetEnabled(false) })
	debug.Clear()

	debug.Record("a", debug.LevelInfo, "no payload")
	withPayloadID := debug.Recordp("a", debug.LevelInfo, "has payload", "the body")
	debug.Record("a", debug.LevelInfo, "no payload either")

	e, ok := debug.Latest()
	if !ok {
		t.Fatal("Latest returned not-ok")
	}
	if e.ID != withPayloadID {
		t.Errorf("Latest = #%d, want #%d (most recent with payload)", e.ID, withPayloadID)
	}
}
