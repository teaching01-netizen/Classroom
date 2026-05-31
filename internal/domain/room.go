package domain

import (
	"encoding/json"
	"fmt"
	"time"
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

var allowedTransitions = map[RoomStatus][]RoomStatus{
	Idle:        {Running, Stopped},
	Running:     {Fetching, Stopped},
	Fetching:    {Running, Warning, AuthExpired, Stopped},
	Warning:     {Fetching, Stopped},
	AuthExpired: {Fetching, Stopped},
	Stopped:     {Running},
}

func (s RoomStatus) CanTransitionTo(next RoomStatus) error {
	for _, valid := range allowedTransitions[s] {
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
	ErrKindRateLimited
	ErrKindAuthConflict
	ErrKindPoolExhausted
)

var ErrAuthExpired = &FetchError{Kind: ErrKindAuthExpired}
var ErrRateLimited = &FetchError{Kind: ErrKindRateLimited}
var ErrAuthConflict = &FetchError{Kind: ErrKindAuthConflict}
var ErrPoolExhausted = &FetchError{Kind: ErrKindPoolExhausted}

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
	case ErrKindRateLimited:
		return "warwick rate limit exceeded"
	case ErrKindAuthConflict:
		return "warwick auth conflict — human admin likely logged in"
	case ErrKindPoolExhausted:
		return "pool exhausted — all sessions in use"
	default:
		return "unknown fetch error"
	}
}

func (e *FetchError) ToRoomStatus() RoomStatus {
	switch e.Kind {
	case ErrKindAuthExpired:
		return AuthExpired
	case ErrKindRateLimited:
		return Warning
	case ErrKindAuthConflict:
		return Warning
	case ErrKindPoolExhausted:
		return Warning
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
	RoomID         string     `json:"room_id"`
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

// RoomLite is a stripped-down version of Room returned when the `lite` query
// parameter is set. It contains only the fields the frontend needs for
// auto-start room existence checks, keeping the payload small.
type RoomLite struct {
	RoomID    string     `json:"room_id"`
	ClassID   string     `json:"class_id"`
	Name      *string    `json:"name"`
	Status    RoomStatus `json:"status"`
	QRURL     *string    `json:"qr_url"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func NewRoom(roomID string, classID string, name *string) Room {
	return Room{
		RoomID:  roomID,
		ClassID: classID,
		Name:    name,
		Status:  Idle,
	}
}

func (r *Room) TransitionTo(next RoomStatus) error {
	if err := r.Status.CanTransitionTo(next); err != nil {
		return err
	}
	r.Status = next
	return nil
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
