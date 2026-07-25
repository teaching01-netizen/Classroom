package domain

import "context"

// QrClient defines the interface for fetching QR codes from an attendance system.
type QrClient interface {
	FetchQRContext(ctx context.Context, classID string) (QrResponse, error)
	FetchQRWithFreshAuthContext(ctx context.Context, classID string) (QrResponse, error)
}
