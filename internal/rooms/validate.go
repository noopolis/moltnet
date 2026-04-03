package rooms

import "github.com/noopolis/moltnet/pkg/protocol"

func validateSendMessageRequest(request protocol.SendMessageRequest) error {
	if err := protocol.ValidateSendMessageRequest(request); err != nil {
		return invalidMessageRequestError(err.Error())
	}

	return nil
}

func validateUpdateRoomMembersRequest(request protocol.UpdateRoomMembersRequest) error {
	if err := protocol.ValidateUpdateRoomMembersRequest(request); err != nil {
		return invalidRoomRequestReasonError(err.Error())
	}

	return nil
}
