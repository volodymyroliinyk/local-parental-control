package webfilter

import (
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNftScriptUsesPerUserPorts(t *testing.T) {
	script := nftScript(map[uint32]int{1001: 53001, 1000: 53000})
	want := []string{
		"destroy table inet local_parental_control",
		"meta skuid 1000 udp dport 53 redirect to :53000",
		"meta skuid 1000 tcp dport 53 redirect to :53000",
		"meta skuid 1001 udp dport 53 redirect to :53001",
	}
	for _, fragment := range want {
		if !strings.Contains(script, fragment) {
			t.Errorf("script does not contain %q:\n%s", fragment, script)
		}
	}
	if strings.Index(script, "skuid 1000") > strings.Index(script, "skuid 1001") {
		t.Fatal("rules are not deterministic")
	}
}

func TestServerAnswersBlockedDomainOverUDPAndTCP(t *testing.T) {
	probe, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4zero, Port: 0})
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("network namespaces are unavailable in this test environment: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	srv, err := startServer(port, "127.0.0.1:1", map[string]struct{}{"example.com": {}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()

	packet := query("www.example.com")
	udp, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(time.Second))
	if _, err := udp.Write(packet); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, maxDNSPacket)
	n, err := udp.Read(response)
	if err != nil || binary.BigEndian.Uint16(response[2:4])&0xf != 3 {
		t.Fatalf("UDP response length=%d error=%v flags=%x", n, err, response[2:4])
	}

	tcp, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	framed, _ := tcpPacket(packet)
	_ = tcp.SetDeadline(time.Now().Add(time.Second))
	if _, err := tcp.Write(framed); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(tcp, response[:2]); err != nil {
		t.Fatal(err)
	}
	length := int(binary.BigEndian.Uint16(response[:2]))
	if _, err := io.ReadFull(tcp, response[:length]); err != nil || binary.BigEndian.Uint16(response[2:4])&0xf != 3 {
		t.Fatalf("TCP response error=%v flags=%x", err, response[2:4])
	}
}

func TestNftScriptWithoutRulesOnlyRemovesOwnedTable(t *testing.T) {
	if got, want := nftScript(nil), "destroy table inet local_parental_control\n"; got != want {
		t.Fatalf("nftScript(nil) = %q, want %q", got, want)
	}
}

func TestSystemResolver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte("# generated\nnameserver 127.0.0.53\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := systemResolver(path)
	if err != nil || got != "127.0.0.53:53" {
		t.Fatalf("systemResolver() = %q, %v", got, err)
	}
}
