package exitcode

const (
	Success    = 0
	Generic    = 1
	Usage      = 64
	Auth       = 77
	NotFound   = 74
	Validation = 65
	Server     = 70
	Network    = 69
	Conflict   = 73
)

type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func Wrap(code int, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Err: err}
}
