package validation

import "regexp"

func IsJsFunctionName(str string) bool {
	m, _ := regexp.MatchString(`^[$a-zA-Z_][a-zA-Z0-9_$]*$`, str)
	return m
}
