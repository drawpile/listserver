package db

type RefreshError struct {
	message string
}

func (e RefreshError) Error() string {
	return e.message
}
