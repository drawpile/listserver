package db

import (
	"crypto/rand"
	"encoding/base64"
)

func generateUpdateKey() (string, error) {
	keybytes := make([]byte, 128/8)
	_, err := rand.Read(keybytes)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(keybytes), nil
}

func generateRoomcode(length int) (string, error) {
	randbytes := make([]byte, length)
	_, err := rand.Read(randbytes)
	if err != nil {
		return "", err
	}
	code := make([]rune, length)
	for i := range randbytes {
		code[i] = rune(randbytes[i]%26) + 'A'
	}
	return string(code), nil
}

func optStringList(fields map[string]interface{}, name string) ([]string, bool) {
	if value, ok := fields[name]; ok {
		val, ok := value.([]string)
		return val, ok
	}
	return []string{}, false
}
func optString(fields map[string]interface{}, name string) (string, bool) {
	if value, ok := fields[name]; ok {
		val, ok := value.(string)
		return val, ok
	}
	return "", false
}

func optInt(fields map[string]interface{}, name string) (int, bool) {
	if value, ok := fields[name]; ok {
		switch v := value.(type) {
		case float64:
			return int(v), true
		case int:
			return v, true
		default:
			return 0, false
		}
	}
	return 0, false
}

func optBool(fields map[string]interface{}, name string) (bool, bool) {
	if value, ok := fields[name]; ok {
		val, ok := value.(bool)
		return val, ok
	}
	return false, false
}
