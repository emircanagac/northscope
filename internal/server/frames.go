package server

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/emircanagac/northscope/internal/models"
)

// snapshotFrames encodes each published topology snapshot once and shares it
// with every WebSocket client. A PreparedMessage also caches its compressed
// frame, so the JSON encoding and the compression cost are paid once per
// snapshot instead of once per browser.
type snapshotFrames struct {
	mu       sync.Mutex
	instance string
	version  int64
	prepared *websocket.PreparedMessage
}

// frame returns the prepared message for snapshot. Only the newest snapshot of
// the current instance is cached: a client that still drains an older one gets
// it encoded on its own without evicting the newer frame.
func (f *snapshotFrames) frame(snapshot models.TopologySnapshot) (*websocket.PreparedMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.prepared != nil && f.instance == snapshot.Instance && f.version == snapshot.Version {
		return f.prepared, nil
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	prepared, err := websocket.NewPreparedMessage(websocket.TextMessage, data)
	if err != nil {
		return nil, err
	}
	if f.prepared == nil || f.instance != snapshot.Instance || snapshot.Version > f.version {
		f.instance, f.version, f.prepared = snapshot.Instance, snapshot.Version, prepared
	}
	return prepared, nil
}
