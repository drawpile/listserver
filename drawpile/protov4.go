package drawpile

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"time"
)

/**
 * Attempt to connect to a Drawpile server
 *
 * Returns nil if the connection succeeds and there is a Drawpile server on the other end
 */
func TryProtoV4Login(address string) error {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		switch t := err.(type) {
		case net.Error:
			if t.Timeout() {
				return errors.New("Connection timed out while trying to connect to " + address + ". Your session does not seem to be accessible over the Internet. Check the hosting help page at drawpile.net")
			}

			// TODO: How the hell do you identify the actual error type?
		}

		log.Println("An error occurred while trying to check connectivity to ", address, ": ", err)
		return errors.New("Your session does not seem to be accessible over the Internet. Check the hosting help page at drawpile.net")
	}

	// Connection established. We should now receive a greeting message letting us know this is a Drawpile server
	defer conn.Close()

	// Expect a greeting message
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	greeting, err := readMessage(conn)

	if err != nil {
		log.Println(address, ": V4 protocol check read error: ", err)
		return errors.New("Your server does not seem to be a supported Drawpile server. Check the hosting help page at drawpile.net")
	}

	if !isValidV4Greeting(greeting) {
		return errors.New("Your server does not seem to be a supported Drawpile server. Check the hosting help page at drawpile.net")
	}

	return nil
}

func readMessage(conn net.Conn) ([]byte, error) {
	header := make([]byte, 4)

	// Read fixed header
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}

	msgLen := binary.BigEndian.Uint16(header[0:2])

	// Read message body
	payload := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

type GreetingMessage struct {
	Version int    `json:"version"`
	Type    string `json:"type"`
}

func isValidV4Greeting(message []byte) bool {
	var greeting GreetingMessage

	if err := json.Unmarshal(message, &greeting); err != nil {
		return false
	}

	return greeting.Version == 4 && greeting.Type == "login"
}
