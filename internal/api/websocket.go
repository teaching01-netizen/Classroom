package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"nhooyr.io/websocket"

	"qr-command-center/internal/service"
)

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

func wsHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			slog.Error("websocket accept failed", "error", err)
			return
		}

		client := &wsClient{
			conn: conn,
			send: make(chan []byte, 256),
		}

		events := rm.Subscribe()

		// Send FullStateSync
		rooms := rm.GetAllRooms()
		syncData := marshalEvent(service.RoomManagerEvent{Type: "FullStateSync", Data: rooms})
		select {
		case client.send <- syncData:
		default:
		}

		var wg sync.WaitGroup
		wg.Add(2)

		// Write pump
		go func() {
			defer wg.Done()
			for msg := range client.send {
				ctx, cancel := context.WithTimeout(r.Context(), 10000)
				err := client.conn.Write(ctx, websocket.MessageText, msg)
				cancel()
				if err != nil {
					slog.Error("ws write failed", "error", err)
					return
				}
			}
		}()

		// Read pump (just wait for close)
		go func() {
			defer wg.Done()
			for {
				_, _, err := client.conn.Read(r.Context())
				if err != nil {
					return
				}
			}
		}()

		// Event forwarder
		go func() {
			for event := range events {
				data := marshalEvent(event)
				select {
				case client.send <- data:
				default:
					slog.Warn("dropping event for slow ws client")
				}
			}
			close(client.send)
		}()

		wg.Wait()
		client.conn.Close(websocket.StatusNormalClosure, "done")
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
