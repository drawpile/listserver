package drawpile

import (
	"regexp"
	"log"
	"strconv"
)

func TryDrawpileLogin(address string, protocol string) error {
	re := regexp.MustCompile(`^\w+:(\d+)\.\d+\.\d+$`)
	m := re.FindStringSubmatch(protocol)
	if len(m) != 2 {
		// Invalid protocol version: we certainly don't support this
		log.Println("Can't check server, invalid protocol", protocol)
		return nil
	}

	version, _ := strconv.Atoi(m[1])

	if version == 4 {
		return TryProtoV4Login(address)
	} else {
		log.Println("Unsupported server protocol version", version)
		return nil
	}
}

