package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"

	"qr-command-center/internal/service"
)

var (
	wsConnCount atomic.Int64
	wsMaxConns  int64
)

func init() {
	wsMaxConns = int64(envOrDefaultInt("WARWICK_MAX_CONCURRENT_WS", 500))
}

// envOrDefaultInt parses an integer from an env var, falling back to defaultVal.
func envOrDefaultInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	if n <= 0 {
		return defaultVal
	}
	return n
}

func wsHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wsConnCount.Load() >= wsMaxConns {
			http.Error(w, "too many WebSocket connections", http.StatusServiceUnavailable)
			return
		}
		wsConnCount.Add(1)
		defer wsConnCount.Add(-1)

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			slog.Error("ws accept failed", "error", err)
			return
		}
		defer conn.CloseNow()
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "done")
		}()

		ctx := conn.CloseRead(r.Context())
		events, unsub := rm.Subscribe()
		defer unsub()

		// Send FullStateSync
		rooms := rm.GetAllRooms()
		syncData := marshalEvent(service.RoomManagerEvent{Type: "FullStateSync", Data: rooms})
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = conn.Write(writeCtx, websocket.MessageText, syncData)
		cancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
				slog.Error("ws write failed", "error", err)
			}
			return
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
				data := marshalEvent(event)
				writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, data)
				cancel()
				if err != nil {
					if !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
						slog.Error("ws write failed", "error", err)
					}
					return
				}
			}
		}
	}
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
