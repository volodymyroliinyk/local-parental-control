package webfilter

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const (
	dnsHeaderSize = 12
	maxDNSPacket  = 65535
)

func questionName(packet []byte) (string, error) {
	if len(packet) < dnsHeaderSize || binary.BigEndian.Uint16(packet[4:6]) != 1 {
		return "", errors.New("DNS packet must contain exactly one question")
	}
	name, next, err := decodeName(packet, dnsHeaderSize, 0)
	if err != nil {
		return "", err
	}
	if next+4 > len(packet) {
		return "", errors.New("truncated DNS question")
	}
	return strings.ToLower(strings.TrimSuffix(name, ".")), nil
}

func decodeName(packet []byte, offset, depth int) (string, int, error) {
	if depth > 16 || offset >= len(packet) {
		return "", 0, errors.New("invalid DNS name")
	}
	labels := make([]string, 0, 4)
	next := offset
	jumped := false
	for {
		if offset >= len(packet) {
			return "", 0, errors.New("truncated DNS name")
		}
		length := int(packet[offset])
		offset++
		if length == 0 {
			if !jumped {
				next = offset
			}
			return strings.Join(labels, "."), next, nil
		}
		if length&0xc0 == 0xc0 {
			if offset >= len(packet) {
				return "", 0, errors.New("truncated DNS compression pointer")
			}
			pointer := ((length & 0x3f) << 8) | int(packet[offset])
			offset++
			if !jumped {
				next = offset
				jumped = true
			}
			suffix, _, err := decodeName(packet, pointer, depth+1)
			if err != nil {
				return "", 0, err
			}
			if suffix != "" {
				labels = append(labels, suffix)
			}
			return strings.Join(labels, "."), next, nil
		}
		if length&0xc0 != 0 || length > 63 || offset+length > len(packet) {
			return "", 0, errors.New("invalid DNS label")
		}
		labels = append(labels, string(packet[offset:offset+length]))
		offset += length
		if !jumped {
			next = offset
		}
	}
}

func blockedResponse(query []byte) ([]byte, error) {
	if _, err := questionName(query); err != nil {
		return nil, err
	}
	response := append([]byte(nil), query...)
	flags := binary.BigEndian.Uint16(response[2:4])
	flags = (flags | 0x8000 | 0x0080) &^ 0x000f // response, recursion available
	flags |= 3                                  // NXDOMAIN
	binary.BigEndian.PutUint16(response[2:4], flags)
	binary.BigEndian.PutUint16(response[6:8], 0)
	binary.BigEndian.PutUint16(response[8:10], 0)
	binary.BigEndian.PutUint16(response[10:12], 0)
	return response, nil
}

func matchesDomain(name string, blocked map[string]struct{}) bool {
	for {
		if _, ok := blocked[name]; ok {
			return true
		}
		dot := strings.IndexByte(name, '.')
		if dot < 0 {
			return false
		}
		name = name[dot+1:]
	}
}

func tcpPacket(packet []byte) ([]byte, error) {
	if len(packet) > maxDNSPacket {
		return nil, fmt.Errorf("DNS response is too large: %d bytes", len(packet))
	}
	framed := make([]byte, len(packet)+2)
	binary.BigEndian.PutUint16(framed[:2], uint16(len(packet)))
	copy(framed[2:], packet)
	return framed, nil
}
