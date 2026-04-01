package scenario

import (
	"testing"

	"max_bot/internal/reporting"
)

func TestApplyStateLayers(t *testing.T) {
	tests := []struct {
		name         string
		state        BotState
		wantUser     reporting.UserStage
		wantMessage  reporting.MessageStage
	}{
		{
			name:        "main menu",
			state:       stateMainMenu,
			wantUser:    reporting.UserStageMainMenu,
			wantMessage: reporting.MessageStageUnset,
		},
		{
			name:        "report category",
			state:       stateReportCategory,
			wantUser:    reporting.UserStageFillingReport,
			wantMessage: reporting.MessageStageCategory,
		},
		{
			name:        "report files",
			state:       stateReportMedia,
			wantUser:    reporting.UserStageFillingReport,
			wantMessage: reporting.MessageStageFiles,
		},
		{
			name:        "my reports list",
			state:       stateMyReportsList,
			wantUser:    reporting.UserStageViewingMessages,
			wantMessage: reporting.MessageStageUnset,
		},
		{
			name:        "clarification requested",
			state:       stateClarificationRequested,
			wantUser:    reporting.UserStageWaitingClarification,
			wantMessage: reporting.MessageStageUnset,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &Session{}
			applyState(session, tt.state)

			if session.State != tt.state {
				t.Fatalf("expected state %q, got %q", tt.state, session.State)
			}
			if session.UserStage != tt.wantUser {
				t.Fatalf("expected user stage %q, got %q", tt.wantUser, session.UserStage)
			}
			if session.MessageStage != tt.wantMessage {
				t.Fatalf("expected message stage %q, got %q", tt.wantMessage, session.MessageStage)
			}
		})
	}
}
