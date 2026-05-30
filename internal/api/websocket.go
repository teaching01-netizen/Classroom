package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"nhooyr.io/websocket"

	"qr-command-center/internal/service"
)

func wsHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			slog.Error("ws accept failed", "error", err)
			return
		}
		defer conn.CloseNow()

		ctx := conn.CloseRead(r.Context())
		events := rm.Subscribe()

		// Send FullStateSync
		rooms := rm.GetAllRooms()
		syncData := marshalEvent(service.RoomManagerEvent{Type: "FullStateSync", Data: rooms})
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = conn.Write(writeCtx, websocket.MessageText, syncData)
		cancel()
		if err != nil {
			if websocket.CloseStatus(err) == -1 {
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
					if websocket.CloseStatus(err) == -1 {
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
