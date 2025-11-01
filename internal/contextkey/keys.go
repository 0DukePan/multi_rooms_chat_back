package contextkey

type contextKey string

const (
	ContextKeyUserID    contextKey = "userID"
	ContextKeyRequestID contextKey = "requestID"
)
