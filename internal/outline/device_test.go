package outline

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
	"golang.org/x/net/dns/dnsmessage"
)

// fakeDNS serves DNS over TCP, answering every A query with 192.0.2.53.
func fakeDNS(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				for {
					var n uint16
					if binary.Read(c, binary.BigEndian, &n) != nil {
						return
					}
					req := make([]byte, n)
					if _, err := io.ReadFull(c, req); err != nil {
						return
					}
					var p dnsmessage.Parser
					h, err := p.Start(req)
					if err != nil {
						return
					}
					q, err := p.Question()
					if err != nil {
						return
					}
					b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, RecursionAvailable: true})
					_ = b.StartQuestions()
					_ = b.Question(q)
					_ = b.StartAnswers()
					if q.Type == dnsmessage.TypeA {
						_ = b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60},
							dnsmessage.AResource{A: [4]byte{192, 0, 2, 53}})
					}
					resp, _ := b.Finish()
					_ = binary.Write(c, binary.BigEndian, uint16(len(resp)))
					_, _ = c.Write(resp)
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// ssServer is a minimal Shadowsocks TCP server that sends every stream to
// upstream, recording the requested destinations. Like outline-ss-server, it
// does not reply to a wrong key: it drains the connection until the client
// gives up.
type ssServer struct {
	addr    netip.AddrPort
	mu      sync.Mutex
	targets []string
}

func newSSServer(t *testing.T, method, password, upstream string) *ssServer {
	t.Helper()
	ciph, err := core.PickCipher(method, nil, password)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &ssServer{addr: netip.MustParseAddrPort(ln.Addr().String())}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := ciph.StreamConn(c)
				tgt, err := socks.ReadAddr(sc)
				if err != nil {
					_, _ = io.Copy(io.Discard, c) // wrong key: stay silent
					return
				}
				s.mu.Lock()
				s.targets = append(s.targets, tgt.String())
				s.mu.Unlock()
				up, err := net.Dial("tcp", upstream)
				if err != nil {
					return // upstream unreachable: close
				}
				defer up.Close()
				go func() { _, _ = io.Copy(up, sc) }()
				_, _ = io.Copy(sc, up)
			}()
		}
	}()
	return s
}

func (s *ssServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.targets...)
}

func TestDeviceCheckTCPThroughShadowsocks(t *testing.T) {
	srv := newSSServer(t, testMethod, "s3cret", fakeDNS(t))
	resolver := netip.MustParseAddr("9.9.9.9")

	for _, prefix := range []string{"", "split:2"} {
		key, err := ParseAccessKey("ss://" + b64url(testMethod+":s3cret") + "@localhost:" + itoa(srv.addr.Port()) + "#test")
		if err != nil {
			t.Fatal(err)
		}
		ip, err := key.ResolveServer(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		dev, err := NewDevice(context.Background(), key.TransportConfig(ip, prefix))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = dev.CheckTCP(ctx, resolver)
		cancel()
		dev.Close()
		if err != nil {
			t.Fatalf("prefix %q: CheckTCP: %v", prefix, err)
		}
	}
	for _, tgt := range srv.seen() {
		if tgt != "9.9.9.9:53" {
			t.Fatalf("server asked to dial %s, want 9.9.9.9:53", tgt)
		}
	}
	if len(srv.seen()) == 0 {
		t.Fatal("no connection reached the server")
	}
}

func TestDeviceCheckTCPWrongPassword(t *testing.T) {
	srv := newSSServer(t, testMethod, "s3cret", fakeDNS(t))
	key := &AccessKey{Method: testMethod, Password: "wrong", Host: "127.0.0.1", Port: srv.addr.Port()}
	dev, err := NewDevice(context.Background(), key.TransportConfig(srv.addr.Addr(), ""))
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	if err := dev.CheckServer(context.Background()); err != nil {
		t.Fatalf("CheckServer: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err = dev.CheckTCP(ctx, netip.MustParseAddr("9.9.9.9"))
	if err == nil {
		t.Fatal("CheckTCP succeeded with a wrong password")
	}
	if !IsTimeout(err) {
		t.Fatalf("wrong key should look like a timeout (login failure), got %v", err)
	}
}

func TestDeviceCheckTCPServerWithoutInternet(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().String()
	ln.Close()
	srv := newSSServer(t, testMethod, "s3cret", dead) // right key, upstream unreachable
	key := &AccessKey{Method: testMethod, Password: "s3cret", Host: "127.0.0.1", Port: srv.addr.Port()}
	dev, err := NewDevice(context.Background(), key.TransportConfig(srv.addr.Addr(), ""))
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = dev.CheckTCP(ctx, netip.MustParseAddr("9.9.9.9"))
	if err == nil || IsTimeout(err) {
		t.Fatalf("want a non-timeout error when the server closes, got %v", err)
	}
}

func TestDeviceCheckTCPUnreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := netip.MustParseAddrPort(ln.Addr().String()).Port()
	ln.Close() // nothing listens here now
	key := &AccessKey{Method: testMethod, Password: "x", Host: "127.0.0.1", Port: port}
	dev, err := NewDevice(context.Background(), key.TransportConfig(netip.MustParseAddr("127.0.0.1"), ""))
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	if err := dev.CheckServer(context.Background()); err == nil {
		t.Fatal("CheckServer succeeded on a closed port")
	}
	err = dev.CheckTCP(context.Background(), netip.MustParseAddr("9.9.9.9"))
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want a fast connection error, got %v", err)
	}
}

func itoa(p uint16) string {
	return netip.AddrPortFrom(netip.IPv4Unspecified(), p).String()[len("0.0.0.0:"):]
}
