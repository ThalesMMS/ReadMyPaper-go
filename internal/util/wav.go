package util

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"
)

// WAVDuration reads RIFF chunks rather than assuming a fixed 44-byte header.
func WAVDuration(path string) (time.Duration, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		return 0, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a RIFF/WAVE file")
	}

	var byteRate uint32
	var dataSize uint32
	for {
		chunk := make([]byte, 8)
		if _, err := io.ReadFull(file, chunk); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return 0, err
		}
		size := binary.LittleEndian.Uint32(chunk[4:])
		switch string(chunk[:4]) {
		case "fmt ":
			payload := make([]byte, size)
			if _, err := io.ReadFull(file, payload); err != nil {
				return 0, err
			}
			if len(payload) >= 12 {
				byteRate = binary.LittleEndian.Uint32(payload[8:12])
			}
		case "data":
			dataSize = size
			if _, err := file.Seek(int64(size), io.SeekCurrent); err != nil {
				return 0, err
			}
		default:
			if _, err := file.Seek(int64(size), io.SeekCurrent); err != nil {
				return 0, err
			}
		}
		if size%2 == 1 {
			if _, err := file.Seek(1, io.SeekCurrent); err != nil {
				return 0, err
			}
		}
		if byteRate > 0 && dataSize > 0 {
			break
		}
	}
	if byteRate == 0 {
		return 0, fmt.Errorf("WAV byte rate not found")
	}
	seconds := float64(dataSize) / float64(byteRate)
	return time.Duration(seconds * float64(time.Second)), nil
}
