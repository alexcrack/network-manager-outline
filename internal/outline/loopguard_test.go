package outline

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"testing"

	"golang.getoutline.org/sdk/network/packetrelay"
	"golang.getoutline.org/sdk/transport"
)

type recordingDialer struct{ dialed []string }

func (d *recordingDialer) DialStream(_ context.Context, raddr string) (transport.StreamConn, error) {
	d.dialed = append(d.dialed, raddr)
	return nil, errors.New("not connected (test)")
}

type recordingSender struct{ sent []netip.AddrPort }

func (s *recordingSender) SendPacket(_ []byte, dst netip.AddrPort) error {
	s.sent = append(s.sent, dst)
	return nil
}
func (s *recordingSender) Close() error { return nil }

type fakeRelay struct{ sender *recordingSender }

func (r fakeRelay) NewAssociation() (packetrelay.PacketSender, packetrelay.PacketReceiver, error) {
	return r.sender, nil, nil
}

func newGuard(server string) *loopGuard {
	return &loopGuard{server: netip.MustParseAddr(server), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestGuardedStreamDialer(t *testing.T) {
	for _, server := range []string{"203.0.113.7", "2001:db8::7"} {
		inner := &recordingDialer{}
		d := guardedStreamDialer{inner, newGuard(server)}

		for _, raddr := range []string{
			netip.AddrPortFrom(netip.MustParseAddr(server), 8388).String(), // the Shadowsocks port
			netip.AddrPortFrom(netip.MustParseAddr(server), 443).String(),  // any port: still a loop
		} {
			if _, err := d.DialStream(context.Background(), raddr); !errors.Is(err, errLoop) {
				t.Errorf("%s: err = %v, want errLoop", raddr, err)
			}
		}
		if server == "203.0.113.7" {
			if _, err := d.DialStream(context.Background(), "[::ffff:203.0.113.7]:8388"); !errors.Is(err, errLoop) {
				t.Errorf("4in6 form not refused: %v", err)
			}
		}
		_, _ = d.DialStream(context.Background(), "198.51.100.1:443")
		_, _ = d.DialStream(context.Background(), "example.com:443")
		if len(inner.dialed) != 2 || inner.dialed[0] != "198.51.100.1:443" || inner.dialed[1] != "example.com:443" {
			t.Errorf("%s: passed through %v", server, inner.dialed)
		}
	}
}

func TestGuardedPacketRelay(t *testing.T) {
	sender := &recordingSender{}
	r := guardedPacketRelay{fakeRelay{sender}, newGuard("203.0.113.7")}
	s, _, err := r.NewAssociation()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SendPacket([]byte("x"), netip.MustParseAddrPort("203.0.113.7:8388")); !errors.Is(err, errLoop) {
		t.Fatalf("err = %v, want errLoop", err)
	}
	if err := s.SendPacket([]byte("x"), netip.MustParseAddrPort("1.1.1.1:53")); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || sender.sent[0].String() != "1.1.1.1:53" {
		t.Fatalf("sent %v", sender.sent)
	}
}

func TestLoopGuardWarnsOncePerEpisode(t *testing.T) {
	var warnings int
	g := &loopGuard{server: netip.MustParseAddr("203.0.113.7"),
		log: slog.New(countingHandler{&warnings})}
	server, other := netip.MustParseAddr("203.0.113.7"), netip.MustParseAddr("1.1.1.1")
	g.blocks(server)
	g.blocks(server) // same episode
	g.blocks(other)  // the route is back
	g.blocks(server) // a new episode
	if warnings != 2 {
		t.Fatalf("warnings = %d, want 2", warnings)
	}
}

type countingHandler struct{ n *int }

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		*h.n++
	}
	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }
