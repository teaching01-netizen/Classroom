package warwick

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func loginPageHTML() []byte {
	return []byte(`<html>
	<body>
		<form method="post" action="/admin/SignIn">
			<input type="password" name="password" />
			<span>Sign In</span>
		</form>
		<input type="hidden" name="__VIEWSTATE" value="abc123" />
	</body>
</html>`)
}

func jsonExpectation(path string) ResponseExpectation {
	return ResponseExpectation{
		AllowedStatuses:    map[int]struct{}{http.StatusOK: {}},
		AllowedMediaTypes:  map[string]struct{}{"application/json": {}},
		ExpectedPathPrefix: path,
	}
}

func jsonResponse(status int, contentType string, contentLength int64, path string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		ContentLength: contentLength,
		Header:        http.Header{"Content-Type": []string{contentType}},
		Request:       &http.Request{URL: &url.URL{Path: path}},
	}
}

func TestValidateBodyFlagsLoginPageReturnedWith200(t *testing.T) {
	guard := NewResponseGuard(1 << 20)
	err := guard.ValidateBody(loginPageHTML(), ResponseExpectation{RequireJSON: true})
	require.ErrorIs(t, err, ErrAuthenticationResponse)
}

func TestValidateMetadataFlagsRedirectOutsideExpectedPrefix(t *testing.T) {
	guard := NewResponseGuard(1 << 20)
	resp := jsonResponse(http.StatusOK, "application/json", 100, "/admin/SignIn")
	err := guard.ValidateMetadata(resp, jsonExpectation("/admin/ClassAttendance"))
	require.ErrorIs(t, err, ErrAuthenticationResponse)
}

func TestValidateMetadataRejectsWrongContentType(t *testing.T) {
	guard := NewResponseGuard(1 << 20)
	resp := jsonResponse(http.StatusOK, "text/html; charset=utf-8", 100, "/admin/ClassAttendance/Fetch")
	err := guard.ValidateMetadata(resp, jsonExpectation("/admin/ClassAttendance"))
	require.ErrorIs(t, err, ErrUnexpectedContentType)
}

func TestValidateMetadataRejectsDisallowedStatus(t *testing.T) {
	guard := NewResponseGuard(1 << 20)
	resp := jsonResponse(http.StatusInternalServerError, "application/json", 100, "/admin/ClassAttendance/Fetch")
	err := guard.ValidateMetadata(resp, jsonExpectation("/admin/ClassAttendance"))
	require.ErrorIs(t, err, ErrUnexpectedHTTPStatus)
}

func TestValidateMetadataRejectsNilResponse(t *testing.T) {
	guard := NewResponseGuard(1 << 20)
	require.Error(t, guard.ValidateMetadata(nil, jsonExpectation("/admin/ClassAttendance")))
}

func TestValidateBodyRejectsOversizedBody(t *testing.T) {
	guard := NewResponseGuard(16)
	err := guard.ValidateBody([]byte(strings.Repeat("x", 17)), ResponseExpectation{})
	require.ErrorIs(t, err, ErrResponseTooLarge)
}

func TestValidateMetadataRejectsOversizedBody(t *testing.T) {
	guard := NewResponseGuard(16)
	resp := jsonResponse(http.StatusOK, "application/json", 17, "/admin/ClassAttendance/Fetch")
	err := guard.ValidateMetadata(resp, jsonExpectation("/admin/ClassAttendance"))
	require.ErrorIs(t, err, ErrResponseTooLarge)
}

func TestValidateBodyFlagsNonJSONWhenJSONRequired(t *testing.T) {
	guard := NewResponseGuard(1 << 20)
	body := []byte(`<html><body><form method="post"><button>Submit</button></form></body></html>`)
	require.NoError(t, guard.ValidateBody(body, ResponseExpectation{}))
	require.ErrorIs(t, guard.ValidateBody(body, ResponseExpectation{RequireJSON: true}), ErrAuthenticationResponse)
}

func TestValidateBodyAndMetadataAcceptValidJSON(t *testing.T) {
	guard := NewResponseGuard(1 << 20)
	body := []byte(`{"class_id":"abc","students":[{"id":1,"name":"A"}]}`)
	require.NoError(t, guard.ValidateBody(body, ResponseExpectation{RequireJSON: true}))

	resp := jsonResponse(http.StatusOK, "application/json; charset=utf-8", int64(len(body)), "/admin/ClassAttendance/ClassAttendanceStudentCheckInSearch")
	require.NoError(t, guard.ValidateMetadata(resp, jsonExpectation("/admin/ClassAttendance")))
}

func TestValidateBodyDoesNotFlagTruncatedJSON(t *testing.T) {
	guard := NewResponseGuard(1 << 20)
	body := []byte(`{"students":[{"id":"1"`)
	// Truncated JSON still starts with '{', so the RequireJSON signal does not fire;
	// truncation is left to the JSON decoder downstream, not the guard.
	require.NoError(t, guard.ValidateBody(body, ResponseExpectation{RequireJSON: true}))
}

func TestNewResponseGuardPanicsOnNonPositiveLimit(t *testing.T) {
	require.Panics(t, func() { NewResponseGuard(0) })
	require.Panics(t, func() { NewResponseGuard(-1) })
}
