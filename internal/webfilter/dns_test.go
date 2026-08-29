package webfilter

import (
	"encoding/binary"
	"strings"
	"testing"
)

func query(name string) []byte {
	packet := make([]byte, dnsHeaderSize)
	binary.BigEndian.PutUint16(packet[0:2], 42)
	binary.BigEndian.PutUint16(packet[2:4], 0x0100)
	binary.BigEndian.PutUint16(packet[4:6], 1)
	for _, label := range strings.Split(name, ".") {
		packet = append(packet, byte(len(label)))
		packet = append(packet, label...)
	}
	packet = append(packet, 0, 0, 1, 0, 1)
	return packet
}

func TestQuestionNameAndBlockedResponse(t *testing.T) {
	packet := query("WWW.Example.COM")
	name, err := questionName(packet)
	if err != nil || name != "www.example.com" {
		t.Fatalf("questionName() = %q, %v", name, err)
	}
	response, err := blockedResponse(packet)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(response[0:2]) != 42 || binary.BigEndian.Uint16(response[2:4])&0x800f != 0x8003 {
		t.Fatalf("invalid NXDOMAIN response header: %x", response[:dnsHeaderSize])
	}
}

func TestMatchesDomainIncludesSubdomainsOnly(t *testing.T) {
	blocked := map[string]struct{}{"example.com": {}}
	for _, name := range []string{"example.com", "www.example.com", "a.b.example.com"} {
		if !matchesDomain(name, blocked) {
			t.Errorf("%q did not match", name)
		}
	}
	for _, name := range []string{"notexample.com", "example.org"} {
		if matchesDomain(name, blocked) {
			t.Errorf("%q matched unexpectedly", name)
		}
	}
}

func TestQuestionNameRejectsMalformedPackets(t *testing.T) {
	for _, packet := range [][]byte{nil, make([]byte, dnsHeaderSize), append(query("example.com")[:13], 64)} {
		if _, err := questionName(packet); err == nil {
			t.Errorf("accepted malformed packet %x", packet)
		}
	}
}
