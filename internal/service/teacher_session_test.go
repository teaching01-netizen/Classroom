package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"qr-command-center/internal/domain"
)

func TestGetSessionDetailDefaultsLegacyEmptyStatus(t *testing.T) {
	// Given
	provider := newMockProvider()
	provider.sessionDetailReturn = &domain.SessionDetail{
		SessionSummary: domain.SessionSummary{SessionID: "session-1"},
	}
	service := NewTeacherService(provider, provider, 2)

	// When
	result, err := service.GetSessionDetail(context.Background(), "course-1", "session-1")

	// Then
	require.NoError(t, err)
	require.Equal(t, domain.SessionStatusNotStarted, result.Detail.Status)
}
