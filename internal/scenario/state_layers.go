package scenario

import "max_bot/internal/reporting"

type BotState string

const (
	stateAbout                  BotState = "about"
	stateFeedback               BotState = "feedback"
	stateUnsupportedInput       BotState = "unsupported_input"
	stateLegalInfo              BotState = "legal_info"
	stateMainMenu               BotState = "main_menu"
	stateViolationsList         BotState = "violations_list"
	stateReportConsent          BotState = "report_consent"
	stateReportCategory         BotState = "report_category"
	stateReportMunicipal        BotState = "report_municipality"
	stateReportPhone            BotState = "report_phone"
	stateReportAddress          BotState = "report_address"
	stateReportTime             BotState = "report_time"
	stateReportDesc             BotState = "report_description"
	stateReportMedia            BotState = "report_media"
	stateReportExtra            BotState = "report_extra"
	stateReportConfirm          BotState = "report_confirm"
	stateReportAccepted         BotState = "report_accepted"
	stateMyReportsList          BotState = "my_reports_list"
	stateMyReportDetail         BotState = "my_report_detail"
	stateReportInProgressNotice BotState = "report_in_progress_notice"
	stateReportRejectedNotice   BotState = "report_rejected_notice"
	stateClarificationRequested BotState = "clarification_requested"
	stateReportResolvedNotice   BotState = "report_resolved_notice"
)

func deriveUserStage(state BotState) reporting.UserStage {
	switch state {
	case stateMyReportsList, stateMyReportDetail:
		return reporting.UserStageViewingMessages
	case stateClarificationRequested:
		return reporting.UserStageWaitingClarification
	case stateReportCategory,
		stateReportMunicipal,
		stateReportPhone,
		stateReportAddress,
		stateReportTime,
		stateReportDesc,
		stateReportMedia,
		stateReportExtra,
		stateReportConfirm,
		stateReportAccepted:
		return reporting.UserStageFillingReport
	default:
		return reporting.UserStageMainMenu
	}
}

func deriveMessageStage(state BotState) reporting.MessageStage {
	switch state {
	case stateReportCategory:
		return reporting.MessageStageCategory
	case stateReportMunicipal:
		return reporting.MessageStageMunicipality
	case stateReportPhone:
		return reporting.MessageStagePhone
	case stateReportAddress:
		return reporting.MessageStageAddress
	case stateReportTime:
		return reporting.MessageStageTime
	case stateReportDesc:
		return reporting.MessageStageDescription
	case stateReportMedia:
		return reporting.MessageStageFiles
	case stateReportExtra:
		return reporting.MessageStageAdditional
	case stateReportAccepted:
		return reporting.MessageStageSended
	default:
		return reporting.MessageStageUnset
	}
}

func applyState(session *Session, state BotState) {
	session.State = state
	session.UserStage = deriveUserStage(state)
	session.MessageStage = deriveMessageStage(state)
}

func stateFromConversation(state reporting.UserStage, messageStage reporting.MessageStage) BotState {
	switch state {
	case reporting.UserStageViewingMessages:
		return stateMyReportsList
	case reporting.UserStageWaitingClarification:
		return stateClarificationRequested
	case reporting.UserStageFillingReport:
		switch messageStage {
		case reporting.MessageStageCategory:
			return stateReportCategory
		case reporting.MessageStageMunicipality:
			return stateReportMunicipal
		case reporting.MessageStagePhone:
			return stateReportPhone
		case reporting.MessageStageAddress:
			return stateReportAddress
		case reporting.MessageStageTime:
			return stateReportTime
		case reporting.MessageStageDescription:
			return stateReportDesc
		case reporting.MessageStageFiles:
			return stateReportMedia
		case reporting.MessageStageAdditional:
			return stateReportExtra
		case reporting.MessageStageSended:
			return stateReportAccepted
		default:
			return stateReportConsent
		}
	default:
		return stateMainMenu
	}
}
