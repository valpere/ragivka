package telegram

// Update mirrors the subset of Telegram's Bot API Update object this
// adapter cares about (https://core.telegram.org/bots/api#update).
// Only text messages in private/group chats are handled — other update
// types (edited_message, callback_query, etc.) are decoded as zero values
// and ignored by the handler.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message mirrors the subset of Telegram's Message object needed to route
// a chat turn through the orchestrator and reply.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

// User mirrors Telegram's User object.
type User struct {
	ID int64 `json:"id"`
}

// Chat mirrors Telegram's Chat object.
type Chat struct {
	ID int64 `json:"id"`
}
