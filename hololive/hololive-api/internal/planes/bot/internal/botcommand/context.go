package botcommand

type CommandContext struct {
	Room      string
	ThreadID  *string
	UserID    string
	UserName  string
	Message   string
	MessageID string
}

func NewCommandContext(room, userID, userName, message string) *CommandContext {
	return &CommandContext{
		Room:     room,
		UserID:   userID,
		UserName: userName,
		Message:  message,
	}
}
