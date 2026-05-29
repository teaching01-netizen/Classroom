package domain

// QrClient defines the interface for fetching QR codes from an attendance system.
type QrClient interface {
	FetchQR(classID string) (QrResponse, error)
	FetchQRWithFreshAuth(classID string) (QrResponse, error)
}