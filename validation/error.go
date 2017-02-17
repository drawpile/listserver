package validation

type ValidationError struct {
	field   string
	message string
}

func (e ValidationError) Error() string {
	return e.field + ": " + e.message
}
