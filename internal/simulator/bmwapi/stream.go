package bmwapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// Stream is CarData's MQTT stream of the account: a broker the car's changes are
// published to, under <GCID>/<VIN>, as BMW's streaming guide describes it.
// Without TLS: the client is given its address. Its rules are BMW's:
//
//   - the username is the GCID, the password an ID token with the stream's scope; a
//     refused one gets CONNACK 4 (bad username or password);
//   - one connection at a time per account: a second gets CONNACK 5 (not authorized),
//     even with a valid token;
//   - the connection is closed when the ID token it was opened with expires;
//   - a client may subscribe to its own topics only, <GCID>/<VIN> or <GCID>/+;
//   - on subscribing, it is sent the car's current values (assumed: reported by the
//     integrations, not by BMW).
type Stream struct {
	h   *Handler
	srv *mochi.Server
	log *slog.Logger

	mu      sync.Mutex
	conn    *mochi.Client // the connection that holds the account's slot
	expires time.Time     // when its ID token expires, on the simulated clock
}

// NewStream creates the stream of h's account, served on ln.
func NewStream(h *Handler, ln net.Listener, log *slog.Logger) (*Stream, error) {
	srv := mochi.New(&mochi.Options{InlineClient: true, Logger: log})
	s := &Stream{h: h, srv: srv, log: log}
	if err := srv.AddHook(&hook{s: s}, nil); err != nil {
		return nil, fmt.Errorf("stream hook: %w", err)
	}
	if err := srv.AddListener(listeners.NewNet("cardata", ln)); err != nil {
		return nil, fmt.Errorf("stream listener: %w", err)
	}
	h.mu.Lock()
	h.stream = s
	h.mu.Unlock()
	return s, nil
}

// Run serves the stream until ctx is done: every tick of real time, the cars' changes
// are published, and a connection whose ID token expired is closed.
func (s *Stream) Run(ctx context.Context, tick time.Duration) error {
	if err := s.srv.Serve(); err != nil {
		return fmt.Errorf("stream: %w", err)
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := s.srv.Close(); err != nil {
				return fmt.Errorf("stream: %w", err)
			}
			return nil
		case <-t.C:
			s.Step()
		}
	}
}

// Step brings the cars up to the simulated clock, publishes what they sent since the
// last step, and closes a connection whose ID token expired.
func (s *Stream) Step() {
	now := s.h.clk.Now()
	s.h.observe(now)
	s.publish(false)
	s.mu.Lock()
	cl, expired := s.conn, s.conn != nil && !now.Before(s.expires)
	s.mu.Unlock()
	if expired {
		cl.Stop(errors.New("ID token expired"))
		s.release(cl)
	}
}

// publish publishes the descriptors each car sent since the last publication, or all
// of its current values (all).
func (s *Stream) publish(all bool) {
	type message struct {
		vin  string
		data map[string]any
	}
	var msgs []message
	s.h.mu.Lock()
	for _, c := range s.h.cars {
		data := map[string]any{}
		for k, v := range c.known {
			if all || c.pending[k] {
				d := map[string]any{"value": v.val.v, "timestamp": stamp(v.at)}
				if v.val.unit != "" {
					d["unit"] = v.val.unit
				}
				data[k] = d
			}
		}
		clear(c.pending)
		if len(data) > 0 {
			msgs = append(msgs, message{c.src.VIN(), data})
		}
	}
	now := s.h.clk.Now()
	s.h.mu.Unlock()
	for _, m := range msgs {
		payload, err := json.Marshal(map[string]any{
			"vin": m.vin, "entityId": nonce(), "topic": m.vin, "timestamp": stamp(now), "data": m.data,
		})
		if err != nil {
			continue // only maps of strings, numbers and booleans
		}
		if err := s.srv.Publish(GCID+"/"+m.vin, payload, false, 1); err != nil {
			s.log.Warn("stream publish", "error", err)
		}
	}
}

// drop closes the connection that holds the slot, as BMW's broker sometimes does.
func (s *Stream) drop() {
	s.mu.Lock()
	cl := s.conn
	s.mu.Unlock()
	if cl != nil {
		cl.Stop(errors.New("dropped"))
		s.release(cl)
	}
}

// release frees the slot cl holds.
func (s *Stream) release(cl *mochi.Client) {
	s.mu.Lock()
	if s.conn == cl {
		s.conn = nil
	}
	s.mu.Unlock()
}

// hook applies BMW's rules to the broker.
type hook struct {
	mochi.HookBase
	s *Stream
}

func (*hook) ID() string { return "cardata" }

func (*hook) Provides(b byte) bool {
	return slices.Contains([]byte{
		mochi.OnConnect, mochi.OnConnectAuthenticate, mochi.OnACLCheck, mochi.OnDisconnect, mochi.OnSubscribed,
	}, b)
}

// OnConnect checks the account and its ID token, and that no other connection holds
// the account's slot. It answers the refusals itself, with MQTT 3.1.1's codes as BMW's
// broker does: mochi would map a bad password to 5.
func (k *hook) OnConnect(cl *mochi.Client, pk packets.Packet) error {
	s := k.s
	exp, ok := idTokenExpiry(string(pk.Connect.Password), string(pk.Connect.Username))
	now := s.h.clk.Now()
	if !ok || !now.Before(exp) {
		_ = s.srv.SendConnack(cl, packets.ErrMalformedUsernameOrPassword, false, nil) // 4 in MQTT 3.1.1
		return packets.ErrBadUsernameOrPassword
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil && !s.conn.Closed() {
		_ = s.srv.SendConnack(cl, packets.Err3NotAuthorized, false, nil)
		return packets.ErrNotAuthorized
	}
	s.conn, s.expires = cl, exp
	return nil
}

// OnConnectAuthenticate accepts the connections OnConnect let through.
func (*hook) OnConnectAuthenticate(*mochi.Client, packets.Packet) bool { return true }

// OnACLCheck lets a client read its account's topics only, and write none.
func (*hook) OnACLCheck(_ *mochi.Client, topic string, write bool) bool {
	return !write && strings.HasPrefix(topic, GCID+"/")
}

func (k *hook) OnDisconnect(cl *mochi.Client, _ error, _ bool) { k.s.release(cl) }

// OnSubscribed sends the cars' current values, once the subscription is in place.
func (k *hook) OnSubscribed(*mochi.Client, packets.Packet, []byte) {
	go func() {
		k.s.h.observe(k.s.h.clk.Now())
		k.s.publish(true)
	}()
}
