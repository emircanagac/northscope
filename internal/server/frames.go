package server

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"

	"github.com/emircanagac/northscope/internal/models"
)

// encodedFrame is a prepared WebSocket message and the size of its JSON
// payload before compression.
type encodedFrame struct {
	prepared *websocket.PreparedMessage
	size     int
}

// frameCache keeps the frame for the newest version of the current instance.
type frameCache struct {
	instance string
	version  int64
	frame    encodedFrame
}

func (c *frameCache) get(instance string, version int64) (encodedFrame, bool) {
	if c.frame.prepared != nil && c.instance == instance && c.version == version {
		return c.frame, true
	}
	return encodedFrame{}, false
}

// put caches frame unless a newer version of the same instance is cached: a
// client that still drains an older publication must not evict it.
func (c *frameCache) put(instance string, version int64, frame encodedFrame) {
	if c.frame.prepared == nil || c.instance != instance || version > c.version {
		c.instance, c.version, c.frame = instance, version, frame
	}
}

// snapshotFrames encodes each published snapshot and delta once and shares
// them with every WebSocket client. A PreparedMessage also caches its
// compressed frame, so the JSON encoding and the compression cost are paid
// once per publication instead of once per browser.
type snapshotFrames struct {
	mu        sync.Mutex
	snapshots frameCache
	deltas    frameCache

	// snapshotBytes is the JSON size of the newest full snapshot encoded for
	// a browser.
	snapshotBytes atomic.Int64
}

func (f *snapshotFrames) frame(snapshot models.TopologySnapshot) (encodedFrame, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if frame, ok := f.snapshots.get(snapshot.Instance, snapshot.Version); ok {
		return frame, nil
	}
	frame, err := encodeFrame(snapshot)
	if err != nil {
		return encodedFrame{}, err
	}
	f.snapshots.put(snapshot.Instance, snapshot.Version, frame)
	f.snapshotBytes.Store(int64(frame.size))
	return frame, nil
}

func (f *snapshotFrames) deltaFrame(delta *models.TopologyDelta) (encodedFrame, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if frame, ok := f.deltas.get(delta.Instance, delta.Version); ok {
		return frame, nil
	}
	frame, err := encodeFrame(delta)
	if err != nil {
		return encodedFrame{}, err
	}
	f.deltas.put(delta.Instance, delta.Version, frame)
	return frame, nil
}

func encodeFrame(value any) (encodedFrame, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return encodedFrame{}, err
	}
	prepared, err := websocket.NewPreparedMessage(websocket.TextMessage, data)
	if err != nil {
		return encodedFrame{}, err
	}
	return encodedFrame{prepared: prepared, size: len(data)}, nil
}

// streamCounters count WebSocket messages and their uncompressed payload
// bytes by message type.
type streamCounters struct {
	snapshotMessages, snapshotBytes atomic.Uint64
	deltaMessages, deltaBytes       atomic.Uint64
}

func (c *streamCounters) record(delta bool, size int) {
	if delta {
		c.deltaMessages.Add(1)
		c.deltaBytes.Add(uint64(size))
		return
	}
	c.snapshotMessages.Add(1)
	c.snapshotBytes.Add(uint64(size))
}

// streamPosition is the last snapshot a WebSocket client received.
type streamPosition struct {
	instance string
	version  int64
}

// nextFrame picks what to send a client for publication: the delta when the
// client asked for deltas and already has the delta's base version, else the
// full snapshot.
func (s *Server) nextFrame(publication publication, wantsDeltas bool, position streamPosition) (encodedFrame, bool, error) {
	delta := publication.Delta
	if wantsDeltas && delta != nil && position.instance == delta.Instance && position.version == delta.BaseVersion {
		frame, err := s.frames.deltaFrame(delta)
		return frame, true, err
	}
	frame, err := s.frames.frame(publication.Snapshot)
	return frame, false, err
}
