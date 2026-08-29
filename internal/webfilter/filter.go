package webfilter

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/volodymyroliinyk/local-parental-control/internal/config"
)

const firstPort = 53000

type Controller interface {
	Apply(context.Context, *config.Config, map[uint32]string) error
	Close() error
}

type Filter struct {
	mu         sync.Mutex
	logger     *slog.Logger
	servers    []*server
	runNft     func(context.Context, string) error
	upstream   string
	generation uint64
}

func New(logger *slog.Logger) *Filter {
	return &Filter{logger: logger, runNft: runNft}
}

func (f *Filter) Apply(ctx context.Context, cfg *config.Config, users map[uint32]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	uids := make([]uint32, 0, len(users))
	for uid, username := range users {
		if len(cfg.Users[username].BlockedDomains) > 0 {
			uids = append(uids, uid)
		}
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if len(uids) > 1000 {
		return errors.New("too many users with domain blocking")
	}
	upstream := ""
	if len(uids) > 0 {
		var err error
		upstream, err = systemResolver("/etc/resolv.conf")
		if err != nil {
			return err
		}
	}

	servers := make([]*server, 0, len(uids))
	rules := make(map[uint32]int, len(uids))
	for i, uid := range uids {
		username := users[uid]
		blocked := make(map[string]struct{}, len(cfg.Users[username].BlockedDomains))
		for _, domain := range cfg.Users[username].BlockedDomains {
			blocked[domain] = struct{}{}
		}
		port := firstPort + int((f.generation+1)%2)*1000 + i
		srv, err := startServer(port, upstream, blocked, f.logger)
		if err != nil {
			closeServers(servers)
			return fmt.Errorf("start DNS filter for user %q: %w", username, err)
		}
		servers = append(servers, srv)
		rules[uid] = port
	}
	if err := f.runNft(ctx, nftScript(rules)); err != nil {
		closeServers(servers)
		return fmt.Errorf("install DNS redirection rules: %w", err)
	}
	old := f.servers
	f.servers = servers
	f.upstream = upstream
	f.generation++
	closeServers(old)
	return nil
}

func (f *Filter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := f.runNft(ctx, nftScript(nil))
	closeServers(f.servers)
	f.servers = nil
	return err
}

func nftScript(rules map[uint32]int) string {
	var b strings.Builder
	b.WriteString("destroy table inet local_parental_control\n")
	if len(rules) == 0 {
		return b.String()
	}
	b.WriteString("table inet local_parental_control {\n chain dns_output { type nat hook output priority dstnat; policy accept;\n")
	uids := make([]uint32, 0, len(rules))
	for uid := range rules {
		uids = append(uids, uid)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	for _, uid := range uids {
		fmt.Fprintf(&b, "meta skuid %d udp dport 53 redirect to :%d\n", uid, rules[uid])
		fmt.Fprintf(&b, "meta skuid %d tcp dport 53 redirect to :%d\n", uid, rules[uid])
	}
	b.WriteString("}\n}\n")
	return b.String()
}

func runNft(ctx context.Context, script string) error {
	cmd := exec.CommandContext(ctx, "/usr/sbin/nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil && strings.Contains(string(output), "No such file or directory") && !strings.Contains(script, "table inet") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("nft: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func systemResolver(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open resolver configuration: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			ip := net.ParseIP(strings.Trim(fields[1], "[]"))
			if ip != nil {
				return net.JoinHostPort(ip.String(), "53"), nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read resolver configuration: %w", err)
	}
	return "", errors.New("no IP nameserver found in /etc/resolv.conf")
}

type server struct {
	udp   *net.UDPConn
	tcp   net.Listener
	slots chan struct{}
}

func startServer(port int, upstream string, blocked map[string]struct{}, logger *slog.Logger) (*server, error) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6unspecified, Port: port})
	if err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp", net.JoinHostPort("::", strconv.Itoa(port)))
	if err != nil {
		udp.Close()
		return nil, err
	}
	s := &server{udp: udp, tcp: tcp, slots: make(chan struct{}, 128)}
	go s.serveUDP(upstream, blocked, logger)
	go s.serveTCP(upstream, blocked, logger)
	return s, nil
}

func (s *server) close() {
	s.udp.Close()
	s.tcp.Close()
}

func closeServers(servers []*server) {
	for _, s := range servers {
		s.close()
	}
}

func filterResponse(query []byte, blocked map[string]struct{}) ([]byte, bool) {
	name, err := questionName(query)
	if err != nil || !matchesDomain(name, blocked) {
		return nil, false
	}
	response, err := blockedResponse(query)
	return response, err == nil
}

func (s *server) serveUDP(upstream string, blocked map[string]struct{}, logger *slog.Logger) {
	buffer := make([]byte, maxDNSPacket)
	for {
		n, client, err := s.udp.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		query := append([]byte(nil), buffer[:n]...)
		select {
		case s.slots <- struct{}{}:
		default:
			continue
		}
		go func() {
			defer func() { <-s.slots }()
			if response, ok := filterResponse(query, blocked); ok {
				_, _ = s.udp.WriteToUDP(response, client)
				return
			}
			conn, err := net.DialTimeout("udp", upstream, 3*time.Second)
			if err != nil {
				logger.Warn("DNS upstream unavailable", "error", err)
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err = conn.Write(query); err != nil {
				return
			}
			responseBuffer := make([]byte, maxDNSPacket)
			n, err := conn.Read(responseBuffer)
			if err == nil {
				_, _ = s.udp.WriteToUDP(responseBuffer[:n], client)
			}
		}()
	}
}

func (s *server) serveTCP(upstream string, blocked map[string]struct{}, logger *slog.Logger) {
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			return
		}
		select {
		case s.slots <- struct{}{}:
			go func() {
				defer func() { <-s.slots }()
				handleTCP(conn, upstream, blocked, logger)
			}()
		default:
			conn.Close()
		}
	}
}

func handleTCP(client net.Conn, upstream string, blocked map[string]struct{}, logger *slog.Logger) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(10 * time.Second))
	var length [2]byte
	if _, err := io.ReadFull(client, length[:]); err != nil {
		return
	}
	query := make([]byte, int(binary.BigEndian.Uint16(length[:])))
	if _, err := io.ReadFull(client, query); err != nil {
		return
	}
	if response, ok := filterResponse(query, blocked); ok {
		framed, _ := tcpPacket(response)
		_, _ = client.Write(framed)
		return
	}
	up, err := net.DialTimeout("tcp", upstream, 3*time.Second)
	if err != nil {
		logger.Warn("DNS upstream unavailable", "error", err)
		return
	}
	defer up.Close()
	_ = up.SetDeadline(time.Now().Add(10 * time.Second))
	framed, _ := tcpPacket(query)
	if _, err := up.Write(framed); err == nil {
		_, _ = io.Copy(client, up)
	}
}
