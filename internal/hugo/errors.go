package hugo

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errf(code, message string) error { return &Error{Code: code, Message: message} }
