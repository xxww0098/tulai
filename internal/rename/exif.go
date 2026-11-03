package rename

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

// photoTime reads JPEG EXIF without loading the image into memory.
func photoTime(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	r := io.LimitReader(f, 8<<20)
	var header [2]byte
	if _, err = io.ReadFull(r, header[:]); err != nil || header != [2]byte{0xff, 0xd8} {
		return time.Time{}, errors.New("仅支持带 EXIF 的 JPEG 照片")
	}
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil || header[0] != 0xff || header[1] == 0xda || header[1] == 0xd9 {
			break
		}
		if header[1] == 0x01 || header[1] >= 0xd0 && header[1] <= 0xd8 {
			continue
		}
		marker := header[1]
		if _, err := io.ReadFull(r, header[:]); err != nil {
			break
		}
		size := int(binary.BigEndian.Uint16(header[:])) - 2
		if size < 0 {
			break
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			break
		}
		if marker == 0xe1 && bytes.HasPrefix(data, []byte("Exif\x00\x00")) {
			if value := exifDate(data[6:]); value != "" {
				if taken, err := time.ParseInLocation("2006:01:02 15:04:05", value, time.Local); err == nil {
					return taken, nil
				}
			}
		}
	}
	return time.Time{}, errors.New("没有找到 EXIF 拍摄时间")
}

func exifDate(data []byte) string {
	if len(data) < 8 {
		return ""
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return ""
	}
	if order.Uint16(data[2:4]) != 42 {
		return ""
	}
	readIFD := func(offset uint32, tag uint16) []byte {
		pos := uint64(offset)
		if pos+2 > uint64(len(data)) {
			return nil
		}
		count := uint64(order.Uint16(data[pos : pos+2]))
		pos += 2
		for i := uint64(0); i < count && pos+12 <= uint64(len(data)); i, pos = i+1, pos+12 {
			entry := data[pos : pos+12]
			if order.Uint16(entry[:2]) == tag {
				return entry
			}
		}
		return nil
	}
	root := order.Uint32(data[4:8])
	entry := readIFD(root, 0x8769)
	var date []byte
	if len(entry) != 0 {
		date = readIFD(order.Uint32(entry[8:12]), 0x9003)
	}
	if len(date) == 0 {
		date = readIFD(root, 0x0132)
	}
	if len(date) == 0 || order.Uint16(date[2:4]) != 2 {
		return ""
	}
	count, offset := uint64(order.Uint32(date[4:8])), uint64(order.Uint32(date[8:12]))
	if count < 19 || offset+count > uint64(len(data)) {
		return ""
	}
	return string(data[offset : offset+19])
}
