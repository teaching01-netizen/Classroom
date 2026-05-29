package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type RoomStatus string

const (
	Idle        RoomStatus = "Idle"
	Running     RoomStatus = "Running"
	Fetching    RoomStatus = "Fetching"
	Warning     RoomStatus = "Warning"
	AuthExpired RoomStatus = "AuthExpired"
	Stopped     RoomStatus = "Stopped"
)

func (s RoomStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

func (s *RoomStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch RoomStatus(str) {
	case Idle, Running, Fetching, Warning, AuthExpired, Stopped:
		*s = RoomStatus(str)
	default:
		return fmt.Errorf("unknown room status: %s", str)
	}
	return nil
}

func (s RoomStatus) CanTransitionTo(next RoomStatus) error {
	allowed := map[RoomStatus][]RoomStatus{
		Idle:        {Running, Stopped},
		Running:     {Fetching, Stopped},
		Fetching:    {Running, Warning, AuthExpired, Stopped},
		Warning:     {Fetching, Stopped},
		AuthExpired: {Stopped},
		Stopped:     {Running},
	}
	for _, valid := range allowed[s] {
		if next == valid {
			return nil
		}
	}
	return &TransitionError{From: s, To: next}
}

type TransitionError struct {
	From RoomStatus
	To   RoomStatus
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("cannot transition from %s to %s", e.From, e.To)
}

type FetchErrorKind int

const (
	ErrKindAuthExpired FetchErrorKind = iota
	ErrKindNetwork
	ErrKindInvalidPayload
)

var ErrAuthExpired = &FetchError{Kind: ErrKindAuthExpired}

type FetchError struct {
	Kind    FetchErrorKind
	Message string
}

func (e *FetchError) Error() string {
	switch e.Kind {
	case ErrKindAuthExpired:
		return "warwick session expired"
	case ErrKindNetwork:
		return fmt.Sprintf("network request failed: %s", e.Message)
	case ErrKindInvalidPayload:
		return fmt.Sprintf("invalid response payload: %s", e.Message)
	default:
		return "unknown fetch error"
	}
}

func (e *FetchError) ToRoomStatus() RoomStatus {
	switch e.Kind {
	case ErrKindAuthExpired:
		return AuthExpired
	default:
		return Warning
	}
}

func NewNetworkError(msg string) *FetchError {
	return &FetchError{Kind: ErrKindNetwork, Message: msg}
}

func NewInvalidPayloadError(msg string) *FetchError {
	return &FetchError{Kind: ErrKindInvalidPayload, Message: msg}
}

type Room struct {
	RoomID         uuid.UUID  `json:"room_id"`
	ClassID        string     `json:"class_id"`
	Name           *string    `json:"name"`
	Status         RoomStatus `json:"status"`
	QRURL          *string    `json:"qr_url"`
	ExpiresAt      *time.Time `json:"expires_at"`
	LastUpdatedAt  *time.Time `json:"last_updated_at"`
	WarningMessage *string    `json:"warning_message"`
	ErrorMessage   *string    `json:"error_message"`
	LastFetchAt    *time.Time `json:"last_fetch_at"`
	CreatedAt      time.Time  `json:"-"`
}

func NewRoom(classID string, name *string) Room {
	return Room{
		RoomID:  uuid.New(),
		ClassID: classID,
		Name:    name,
		Status:  Idle,
	}
}

func (r *Room) TransitionTo(next RoomStatus) {
	r.Status = next
}

type QrTime uint64

func (qt *QrTime) UnmarshalJSON(data []byte) error {
	var num uint64
	if err := json.Unmarshal(data, &num); err == nil {
		*qt = QrTime(num)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		n, err := fmt.Sscanf(s, "%d", &num)
		if err != nil || n != 1 {
			return fmt.Errorf("cannot parse qrTime %q as u64", s)
		}
		*qt = QrTime(num)
		return nil
	}
	return fmt.Errorf("qrTime must be number or string, got %s", string(data))
}

type QrResponse struct {
	QrURL  string `json:"qrUrl"`
	QrTime QrTime `json:"qrTime"`
}

func CalculateNextFetchDelay(ttl uint64) uint64 {
	return (ttl * 3) / 4
}
