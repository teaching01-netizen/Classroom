package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/service"
)

var wsConnCount atomic.Int64

func acquireWebSocketSlot(maximum int64) bool {
	if maximum <= 0 {
		return false
	}
	for {
		current := wsConnCount.Load()
		if current >= maximum {
			return false
		}
		if wsConnCount.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func wsHandler(rm *service.RoomManager, maxConns int64, activityRecorder service.ActivityRecorder) http.HandlerFunc {
	var hub *service.EventHub
	if rm != nil {
		hub = rm.EventHub()
	}
	return wsHandlerWithSnapshots(rm, hub, nil, maxConns, activityRecorder)
}

func wsHandlerWithSnapshots(
	rm *service.RoomManager,
	hub *service.EventHub,
	snapshotStore service.SnapshotMetadataStore,
	maxConns int64,
	activityRecorder service.ActivityRecorder,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !acquireWebSocketSlot(maxConns) {
			http.Error(w, "too many WebSocket connections", http.StatusServiceUnavailable)
			return
		}
		defer wsConnCount.Add(-1)

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			slog.Error("ws accept failed", "error", err)
			return
		}
		if activityRecorder != nil {
			activityRecorder.RecordActivity()
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "done")
		}()

		ctx := conn.CloseRead(r.Context())
		if hub == nil {
			slog.Error("ws event hub unavailable")
			return
		}
		// Subscribe before reading either state source so committed events
		// cannot fall into the synchronization gap.
		events, unsub := hub.Subscribe()
		defer unsub()

		var rooms any = []any{}
		if rm != nil {
			rooms = domain.RoomsToLite(rm.GetAllRooms())
		}
		snapshotVersions := make(map[string]int64)
		var snapshotMetadata []domain.SnapshotMetadata
		if snapshotStore != nil {
			var metadataErr error
			snapshotMetadata, metadataErr = snapshotStore.ListMetadata(ctx, time.Now().UTC())
			if metadataErr != nil {
				slog.Error("ws snapshot state sync failed", "error", metadataErr)
				return
			}
			for _, item := range snapshotMetadata {
				key := websocketSnapshotKey(item)
				if item.Version > snapshotVersions[key] {
					snapshotVersions[key] = item.Version
				}
			}
		}

		syncData := marshalEvent(service.RoomManagerEvent{Type: "FullStateSync", Data: rooms})
		if err := writeWebSocketEvent(ctx, conn, syncData); err != nil {
			logWebSocketWriteError(err)
			return
		}

		if snapshotStore != nil {
			stateData := marshalEvent(service.AppEvent{
				Type: "SnapshotStateSync",
				Data: snapshotMetadata,
			})
			if err := writeWebSocketEvent(ctx, conn, stateData); err != nil {
				logWebSocketWriteError(err)
				return
			}
		}

		// Single goroutine: writes events from subscribe channel
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Type == "SnapshotCommitted" {
					metadata, valid := snapshotMetadataFromEvent(event.Data)
					if !valid {
						continue
					}
					key := websocketSnapshotKey(metadata)
					if metadata.Version <= snapshotVersions[key] {
						continue
					}
					snapshotVersions[key] = metadata.Version
				}
				data := marshalEvent(event)
				if err := writeWebSocketEvent(ctx, conn, data); err != nil {
					logWebSocketWriteError(err)
					return
				}
			}
		}
	}
}

func writeWebSocketEvent(
	ctx context.Context,
	connection *websocket.Conn,
	data []byte,
) error {
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return connection.Write(writeCtx, websocket.MessageText, data)
}

func logWebSocketWriteError(err error) {
	if !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
		slog.Error("ws write failed", "error", err)
	}
}

func snapshotMetadataFromEvent(data any) (domain.SnapshotMetadata, bool) {
	switch metadata := data.(type) {
	case domain.SnapshotMetadata:
		return metadata, true
	case *domain.SnapshotMetadata:
		if metadata != nil {
			return *metadata, true
		}
	}
	return domain.SnapshotMetadata{}, false
}

func websocketSnapshotKey(metadata domain.SnapshotMetadata) string {
	return string(metadata.Kind) + "\x00" + metadata.ParentKey + "\x00" + metadata.ResourceKey
}

func marshalEvent(event service.RoomManagerEvent) []byte {
	var wrapper map[string]interface{}
	switch event.Type {
	case "RoomCreated", "RoomUpdated":
		wrapper = map[string]interface{}{event.Type: event.Data}
	case "RoomDeleted":
		wrapper = map[string]interface{}{event.Type: event.Data}
	case "FullStateSync":
		wrapper = map[string]interface{}{event.Type: event.Data}
	default:
		wrapper = map[string]interface{}{event.Type: event.Data}
	}
	data, _ := json.Marshal(wrapper)
	return data
}
