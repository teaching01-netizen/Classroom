package warwick

import (
	"bytes"
	"errors"
	"mime"
	"net/http"
	"strings"
)

const authSignalThreshold = 2

var (
	ErrAuthenticationResponse = errors.New("upstream returned an authentication page")
	ErrUnexpectedContentType  = errors.New("unexpected response content type")
	ErrUnexpectedHTTPStatus   = errors.New("unexpected HTTP status")
	ErrResponseTooLarge       = errors.New("response body exceeds configured limit")
)

type ResponseExpectation struct {
	AllowedStatuses    map[int]struct{}
	AllowedMediaTypes  map[string]struct{}
	ExpectedPathPrefix string
	RequireJSON        bool
}

type ResponseGuard struct {
	MaxBodyBytes int64
}

func NewResponseGuard(maxBodyBytes int64) *ResponseGuard {
	if maxBodyBytes <= 0 {
		panic("ResponseGuard: max body bytes must be positive")
	}
	return &ResponseGuard{MaxBodyBytes: maxBodyBytes}
}

func (g *ResponseGuard) ValidateMetadata(resp *http.Response, expectation ResponseExpectation) error {
	if resp == nil {
		return errors.New("nil HTTP response")
	}
	if _, ok := expectation.AllowedStatuses[resp.StatusCode]; !ok {
		return ErrUnexpectedHTTPStatus
	}
	if resp.ContentLength > g.MaxBodyBytes {
		return ErrResponseTooLarge
	}
	if expectation.ExpectedPathPrefix != "" && !strings.HasPrefix(resp.Request.URL.Path, expectation.ExpectedPathPrefix) {
		return ErrAuthenticationResponse
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return ErrUnexpectedContentType
	}
	if _, ok := expectation.AllowedMediaTypes[mediaType]; !ok {
		return ErrUnexpectedContentType
	}
	return nil
}

func (g *ResponseGuard) ValidateBody(body []byte, expectation ResponseExpectation) error {
	if int64(len(body)) > g.MaxBodyBytes {
		return ErrResponseTooLarge
	}
	if authSignalScore(body, expectation.RequireJSON) >= authSignalThreshold {
		return ErrAuthenticationResponse
	}
	return nil
}

func authSignalsDetected(body []byte) bool {
	return authSignalScore(body, false) >= authSignalThreshold
}

func authSignalScore(body []byte, requireJSON bool) int {
	text := strings.ToLower(string(body))
	score := 0
	if strings.Contains(text, "<form") {
		score++
	}
	if strings.Contains(text, `type="password"`) || strings.Contains(text, `name="password"`) {
		score++
	}
	if strings.Contains(text, "__viewstate") {
		score++
	}
	if strings.Contains(text, "login") || strings.Contains(text, "sign in") {
		score++
	}
	if strings.Contains(text, "idg-box-login-primary") || strings.Contains(text, "idg-btn-sumbit") {
		score += 2
	}
	if requireJSON {
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
			score++
		}
	}
	return score
}
