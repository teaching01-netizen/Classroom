package warwick

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestFetchQRSuccess(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=abc123; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer authServer.Close()

	qrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"qrUrl":"data:image/png;base64,abcd","qrTime":60}`))
	}))
	defer qrServer.Close()

	auth := NewWarwickAuth("test@test.com", "pass", authServer.URL)
	client := NewWarwickQrClientWithEndpoint(auth, qrServer.URL)

	resp, err := client.FetchQR("18248")
	require.NoError(t, err)
	assert.Equal(t, "data:image/png;base64,abcd", resp.QrURL)
	assert.Equal(t, domain.QrTime(60), resp.QrTime)
}

func TestFetchQRAuthExpired302(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=abc123; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer authServer.Close()

	qrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/login")
		w.WriteHeader(http.StatusFound)
	}))
	defer qrServer.Close()

	auth := NewWarwickAuth("test@test.com", "pass", authServer.URL)
	client := NewWarwickQrClientWithEndpoint(auth, qrServer.URL)

	_, err := client.FetchQR("18248")
	assert.Error(t, err)
	var fetchErr *domain.FetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, domain.ErrKindAuthExpired, fetchErr.Kind)
}

func TestFetchQRLoginPageHTML(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=abc123; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer authServer.Close()

	qrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<div class="idg-box-login-primary">Sign In</div>`))
	}))
	defer qrServer.Close()

	auth := NewWarwickAuth("test@test.com", "pass", authServer.URL)
	client := NewWarwickQrClientWithEndpoint(auth, qrServer.URL)

	_, err := client.FetchQR("18248")
	assert.Error(t, err)
	var fetchErr *domain.FetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, domain.ErrKindAuthExpired, fetchErr.Kind)
}

func TestFetchQREmptyQRUrl(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "ASP.NET_SessionId=abc123; path=/; HttpOnly")
		w.WriteHeader(http.StatusFound)
	}))
	defer authServer.Close()

	qrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"qrUrl":"","qrTime":60}`))
	}))
	defer qrServer.Close()

	auth := NewWarwickAuth("test@test.com", "pass", authServer.URL)
	client := NewWarwickQrClientWithEndpoint(auth, qrServer.URL)

	_, err := client.FetchQR("18248")
	assert.Error(t, err)
	var fetchErr *domain.FetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, domain.ErrKindInvalidPayload, fetchErr.Kind)
}
