package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"qr-command-center/internal/domain"
)

type RoomRepository interface {
	CreateRoom(room domain.Room) (domain.Room, error)
	GetRoom(roomID string) (domain.Room, error)
	GetAllRooms() ([]domain.Room, error)
	UpdateRoom(room domain.Room) (domain.Room, error)
	DeleteRoom(roomID string) error
}

type PgRoomRepository struct {
	pool *pgxpool.Pool
}

func NewPgRoomRepository(pool *pgxpool.Pool) *PgRoomRepository {
	return &PgRoomRepository{pool: pool}
}

func (r *PgRoomRepository) CreateRoom(room domain.Room) (domain.Room, error) {
	statusStr := roomStatusToString(room.Status)
	_, err := r.pool.Exec(context.Background(),
		`INSERT INTO rooms (room_id, class_id, name, status, qr_url, expires_at, last_updated_at, warning_message, error_message, last_fetch_at)
		 VALUES ($1, $2, $3, $4::room_status, $5, $6, $7, $8, $9, $10)`,
		room.RoomID, room.ClassID, room.Name, statusStr,
		room.QRURL, room.ExpiresAt, room.LastUpdatedAt,
		room.WarningMessage, room.ErrorMessage, room.LastFetchAt,
	)
	if err != nil {
		return domain.Room{}, fmt.Errorf("create room: %w", err)
	}
	return room, nil
}

func (r *PgRoomRepository) GetRoom(roomID string) (domain.Room, error) {
	row := r.pool.QueryRow(context.Background(),
		`SELECT room_id, class_id, name, status::text, qr_url, expires_at, last_updated_at, warning_message, error_message, last_fetch_at, created_at
		 FROM rooms WHERE room_id = $1`, roomID)
	return scanRoom(row)
}

func (r *PgRoomRepository) GetAllRooms() ([]domain.Room, error) {
	rows, err := r.pool.Query(context.Background(),
		`SELECT room_id, class_id, name, status::text, qr_url, expires_at, last_updated_at, warning_message, error_message, last_fetch_at, created_at
		 FROM rooms`)
	if err != nil {
		return nil, fmt.Errorf("get all rooms: %w", err)
	}
	defer rows.Close()

	var rooms []domain.Room
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, nil
}

func (r *PgRoomRepository) UpdateRoom(room domain.Room) (domain.Room, error) {
	statusStr := roomStatusToString(room.Status)
	result, err := r.pool.Exec(context.Background(),
		`UPDATE rooms SET class_id=$2, name=$3, status=$4::room_status, qr_url=$5, expires_at=$6, last_updated_at=$7, warning_message=$8, error_message=$9, last_fetch_at=$10
		 WHERE room_id = $1`,
		room.RoomID, room.ClassID, room.Name, statusStr,
		room.QRURL, room.ExpiresAt, room.LastUpdatedAt,
		room.WarningMessage, room.ErrorMessage, room.LastFetchAt,
	)
	if err != nil {
		return domain.Room{}, fmt.Errorf("update room: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.Room{}, fmt.Errorf("room not found: %s", room.RoomID)
	}
	return room, nil
}

func (r *PgRoomRepository) DeleteRoom(roomID string) error {
	result, err := r.pool.Exec(context.Background(),
		`DELETE FROM rooms WHERE room_id = $1`, roomID)
	if err != nil {
		return fmt.Errorf("delete room: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("room not found: %s", roomID)
	}
	return nil
}

func roomStatusToString(status domain.RoomStatus) string {
	return strings.ToLower(string(status))
}

func stringToRoomStatus(s string) (domain.RoomStatus, error) {
	switch strings.ToLower(s) {
	case "idle":
		return domain.Idle, nil
	case "running":
		return domain.Running, nil
	case "fetching":
		return domain.Fetching, nil
	case "warning":
		return domain.Warning, nil
	case "auth_expired":
		return domain.AuthExpired, nil
	case "stopped":
		return domain.Stopped, nil
	default:
		return "", fmt.Errorf("unknown room status: %s", s)
	}
}

type scannable interface {
	Scan(dest ...any) error
}

func scanRoom(row scannable) (domain.Room, error) {
	var r domain.Room
	var statusStr string
	err := row.Scan(
		&r.RoomID, &r.ClassID, &r.Name, &statusStr,
		&r.QRURL, &r.ExpiresAt, &r.LastUpdatedAt,
		&r.WarningMessage, &r.ErrorMessage, &r.LastFetchAt, &r.CreatedAt,
	)
	if err != nil {
		return domain.Room{}, err
	}
	r.Status, err = stringToRoomStatus(statusStr)
	if err != nil {
		return domain.Room{}, err
	}
	return r, nil
}

var _ RoomRepository = (*PgRoomRepository)(nil)
