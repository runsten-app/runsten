package auth

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"runsten/internal/platform/clock"
)

var t0 = time.Date(2026, 9, 28, 6, 0, 0, 0, time.UTC)

const password = "correct horse battery staple"

// memStore is an in-memory Store, with the constraints of the PostgreSQL one.
type memStore struct {
	mu       sync.Mutex
	users    map[string]User // by ID
	sessions map[string]storedSession
	tokens   map[string]storedToken
	next     int
	err      error // returned by every call when set
}

type storedSession struct {
	Session
	hash []byte
}

type storedToken struct {
	Token
	hash []byte
}

func newMemStore() *memStore {
	return &memStore{users: map[string]User{}, sessions: map[string]storedSession{}, tokens: map[string]storedToken{}}
}

func (m *memStore) id() string { m.next++; return strconv.Itoa(m.next) }

func (m *memStore) UserByName(_ context.Context, name string) (User, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Username == name {
			return u, true, m.err
		}
	}
	return User{}, false, m.err
}

func (m *memStore) CreateUser(_ context.Context, accountID, name, hash string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	for _, u := range m.users {
		switch {
		case u.Username == name:
			return "", ErrUsernameTaken
		case u.AccountID == accountID:
			return "", ErrAccountHasUser
		}
	}
	u := User{ID: m.id(), AccountID: accountID, Username: name, PasswordHash: hash}
	m.users[u.ID] = u
	return u.ID, nil
}

func (m *memStore) SetPassword(_ context.Context, accountID, userID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok || u.AccountID != accountID {
		return errors.New("no such user")
	}
	u.PasswordHash = hash
	m.users[userID] = u
	for id, s := range m.sessions {
		if s.UserID == userID {
			delete(m.sessions, id)
		}
	}
	return m.err
}

func (m *memStore) CreateSession(_ context.Context, s Session, hash []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	s.ID = m.id()
	m.sessions[s.ID] = storedSession{Session: s, hash: hash}
	return s.ID, nil
}

func (m *memStore) SessionByToken(_ context.Context, hash []byte) (Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if bytes.Equal(s.hash, hash) {
			return s.Session, true, m.err
		}
	}
	return Session{}, false, m.err
}

func (m *memStore) TouchSession(_ context.Context, accountID, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok && s.AccountID == accountID {
		s.LastSeenAt = at
		m.sessions[id] = s
	}
	return m.err
}

func (m *memStore) DeleteSession(_ context.Context, accountID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok && s.AccountID == accountID {
		delete(m.sessions, id)
	}
	return m.err
}

func (m *memStore) PurgeSessions(_ context.Context, accountID string, now, idleSince time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.AccountID == accountID && (!now.Before(s.ExpiresAt) || s.LastSeenAt.Before(idleSince)) {
			delete(m.sessions, id)
		}
	}
	return m.err
}

func (m *memStore) CreateToken(_ context.Context, t Token, hash []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	t.ID = m.id()
	m.tokens[t.ID] = storedToken{Token: t, hash: hash}
	return t.ID, nil
}

func (m *memStore) TokenByHash(_ context.Context, hash []byte) (Token, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if bytes.Equal(t.hash, hash) {
			return t.Token, true, m.err
		}
	}
	return Token{}, false, m.err
}

func (m *memStore) TouchToken(_ context.Context, accountID, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tokens[id]; ok && t.AccountID == accountID {
		t.LastUsedAt = at
		m.tokens[id] = t
	}
	return m.err
}

func (m *memStore) ListTokens(_ context.Context, accountID, userID string) ([]Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Token
	for _, t := range m.tokens {
		if t.AccountID == accountID && t.UserID == userID {
			out = append(out, t.Token)
		}
	}
	slices.SortFunc(out, func(a, b Token) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, m.err
}

func (m *memStore) DeleteToken(_ context.Context, accountID, userID, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[id]
	if !ok || t.AccountID != accountID || t.UserID != userID {
		return false, m.err
	}
	delete(m.tokens, id)
	return true, m.err
}

func (m *memStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// cheap is fast to compute, for tests only.
var cheap = HashParams{MemoryKiB: 64, Iterations: 1, Threads: 1, KeyLen: 32}

func testParams() Params {
	p := DefaultParams()
	p.Hash = cheap
	return p
}

// seeded is a deterministic source of randomness.
func seeded() *rand.ChaCha8 { return rand.NewChaCha8([32]byte{1}) }

func newService(ctx context.Context, t *testing.T) (*Service, *memStore, *clock.Manual) {
	t.Helper()
	st, clk := newMemStore(), clock.NewManual(t0)
	s := New(st, clk, testParams(), seeded())
	if _, err := s.CreateUser(ctx, "acc", "Admin", password); err != nil {
		t.Fatal(err)
	}
	return s, st, clk
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword(password, DefaultHashParams(), seeded())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash = %s", h)
	}
	other, _ := HashPassword(password, DefaultHashParams(), seeded())
	again, _ := HashPassword(password, DefaultHashParams(), rand.NewChaCha8([32]byte{2}))
	if other != h || again == h {
		t.Error("the salt must come from the random source")
	}
	for _, tt := range []struct {
		name, hash, password string
		ok, fails            bool
	}{
		{"right password", h, password, true, false},
		{"wrong password", h, password + "!", false, false},
		{"cost read from the hash", mustHash(t, cheap), password, true, false},
		{"not argon2id", "$2a$12$abcdefghijklmnopqrstuv", password, false, true},
		{"other version", strings.Replace(h, "v=19", "v=16", 1), password, false, true},
		{"bad parameters", strings.Replace(h, "t=2", "t=0", 1), password, false, true},
		{"bad salt", strings.Replace(h, "p=1$", "p=1$!", 1), password, false, true},
		{"no key", h[:strings.LastIndex(h, "$")+1], password, false, true},
	} {
		ok, err := verifyPassword(tt.hash, tt.password)
		if ok != tt.ok || (err != nil) != tt.fails {
			t.Errorf("%s: %v, %v", tt.name, ok, err)
		}
	}
	if _, err := HashPassword(password, cheap, bytes.NewReader(nil)); err == nil {
		t.Error("salt read failure ignored")
	}
}

func mustHash(t *testing.T, p HashParams) string {
	t.Helper()
	h, err := HashPassword(password, p, seeded())
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestValidation(t *testing.T) {
	for name, ok := range map[string]bool{
		"admin": true, " Admin ": true, "driver@example.org": true,
		"": false, "   ": false, "two words": false, "tab\tname": false, strings.Repeat("a", 255): false,
	} {
		if _, err := NormalizeUsername(name); (err == nil) != ok {
			t.Errorf("username %q: %v", name, err)
		}
	}
	if n, _ := NormalizeUsername(" Admin "); n != "admin" {
		t.Errorf("normalized = %q", n)
	}
	for pw, ok := range map[string]bool{
		password: true, "fifteen chars!!": true, "ééééééééééééééé": true, // 15 characters, 30 bytes
		"fourteen chars": false, "\xff\xfe" + password: false, strings.Repeat("a", 1025): false,
	} {
		if err := ValidatePassword(pw); (err == nil) != ok {
			t.Errorf("password %q: %v", pw, err)
		}
	}
}

func TestCreateUser(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newService(ctx, t)
	for _, tt := range []struct {
		name, account, user, password string
		want                          error
	}{
		{"taken, whatever the case", "other", "ADMIN", password, ErrUsernameTaken},
		{"one user per account", "acc", "second", password, ErrAccountHasUser},
		{"short password", "other", "bob", "short", nil},
		{"invalid username", "other", "a b", password, nil},
	} {
		_, err := s.CreateUser(ctx, tt.account, tt.user, tt.password)
		if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
			t.Errorf("%s: %v", tt.name, err)
		}
	}
	u, ok, _ := st.UserByName(ctx, "admin")
	if !ok || !strings.HasPrefix(u.PasswordHash, "$argon2id$") || strings.Contains(u.PasswordHash, password) {
		t.Errorf("stored user = %+v", u)
	}
}

// TestOpen opens a session without a password, as the hosted offer's sign-in does: the
// same session as a login's.
func TestOpen(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newService(ctx, t)
	u, _, _ := s.st.UserByName(ctx, "admin")
	token, sess, err := s.Open(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Authenticate(ctx, token)
	if err != nil || got.ID != sess.ID || got.AccountID != "acc" || got.Username != "admin" {
		t.Errorf("session %+v, %v", got, err)
	}
}

func TestLoginAndSessions(t *testing.T) {
	ctx := context.Background()
	s, st, clk := newService(ctx, t)
	p := testParams()

	if _, _, err := s.Login(ctx, "admin", "wrong password!!", "c"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if _, _, err := s.Login(ctx, "nobody", password, "c"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: %v", err)
	}
	token, sess, err := s.Login(ctx, " ADMIN ", password, "c")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || sess.AccountID != "acc" || sess.Username != "admin" || !sess.ExpiresAt.Equal(t0.Add(p.SessionTTL)) {
		t.Errorf("token %q, session %+v", token, sess)
	}
	for _, stored := range st.sessions {
		if bytes.Contains(stored.hash, []byte(token)) || len(stored.hash) != 32 {
			t.Errorf("the store must keep a hash of the token: %x", stored.hash)
		}
	}

	t.Run("authenticated, activity recorded at most every TouchEvery", func(t *testing.T) {
		clk.Advance(p.TouchEvery / 2)
		got, err := s.Authenticate(ctx, token)
		if err != nil || got.ID != sess.ID || !got.LastSeenAt.Equal(t0) {
			t.Fatalf("session %+v, %v", got, err)
		}
		clk.Advance(p.TouchEvery)
		if got, _ := s.Authenticate(ctx, token); !got.LastSeenAt.Equal(clk.Now()) {
			t.Errorf("last seen %s", got.LastSeenAt)
		}
	})
	t.Run("unknown or malformed tokens", func(t *testing.T) {
		for _, tok := range []string{"", "short", strings.Repeat("A", 43), token + "x"} {
			if _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrNoSession) {
				t.Errorf("%q: %v", tok, err)
			}
		}
	})
	t.Run("idle session expires and is deleted", func(t *testing.T) {
		clk.Advance(p.IdleTTL)
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) || st.count() != 0 {
			t.Errorf("%v, %d sessions left", err, st.count())
		}
	})
	t.Run("absolute expiry despite activity", func(t *testing.T) {
		start := clk.Now()
		token, _, err := s.Login(ctx, "admin", password, "c")
		if err != nil {
			t.Fatal(err)
		}
		for clk.Now().Before(start.Add(p.SessionTTL - p.IdleTTL/2)) {
			clk.Advance(p.IdleTTL / 2)
			if _, err := s.Authenticate(ctx, token); err != nil {
				t.Fatalf("active session ended at %s: %v", clk.Now(), err)
			}
		}
		clk.Advance(p.IdleTTL / 2)
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) {
			t.Errorf("after %s: %v", clk.Now().Sub(start), err)
		}
	})
	t.Run("logout", func(t *testing.T) {
		token, sess, _ := s.Login(ctx, "admin", password, "c")
		if err := s.Logout(ctx, sess); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) {
			t.Errorf("after logout: %v", err)
		}
	})
	t.Run("login purges the expired sessions of the account", func(t *testing.T) {
		_, _, _ = s.Login(ctx, "admin", password, "c")
		clk.Advance(p.IdleTTL + time.Second)
		_, _, _ = s.Login(ctx, "admin", password, "c")
		if st.count() != 1 {
			t.Errorf("%d sessions", st.count())
		}
	})
	t.Run("new password: old sessions end, the new password works", func(t *testing.T) {
		token, _, _ := s.Login(ctx, "admin", password, "c")
		const next = "another long passphrase"
		if err := s.SetPassword(ctx, "Admin", next); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) {
			t.Errorf("old session: %v", err)
		}
		if _, _, err := s.Login(ctx, "admin", password, "c"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("old password: %v", err)
		}
		if _, _, err := s.Login(ctx, "admin", next, "c"); err != nil {
			t.Errorf("new password: %v", err)
		}
		for _, tt := range []struct{ user, pw string }{{"nobody", next}, {"admin", "short"}, {"", next}} {
			if err := s.SetPassword(ctx, tt.user, tt.pw); err == nil {
				t.Errorf("SetPassword(%q, %q) accepted", tt.user, tt.pw)
			}
		}
	})
}

func TestThrottle(t *testing.T) {
	ctx := context.Background()
	p := testParams()
	t.Run("per username, whatever the client", func(t *testing.T) {
		s, _, clk := newService(ctx, t)
		for i := range p.MaxFailures {
			if _, _, err := s.Login(ctx, "admin", "wrong password!!", "c"+strconv.Itoa(i)); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("attempt %d: %v", i, err)
			}
		}
		_, _, err := s.Login(ctx, "admin", password, "fresh")
		te, ok := IsThrottled(err)
		if !ok || te.RetryIn != p.FailureWindow || !strings.Contains(te.Error(), "15m0s") {
			t.Fatalf("blocked login: %v", err)
		}
		// Another user from a fresh client is not blocked.
		if _, _, err := s.Login(ctx, "other", password, "fresh"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("other user: %v", err)
		}
		clk.Advance(p.FailureWindow)
		if _, _, err := s.Login(ctx, "admin", password, "fresh"); err != nil {
			t.Errorf("after the window: %v", err)
		}
	})
	t.Run("per client, whatever the username", func(t *testing.T) {
		s, _, clk := newService(ctx, t)
		for i := range p.MaxFailures {
			_, _, _ = s.Login(ctx, "user"+strconv.Itoa(i), password, "attacker")
		}
		clk.Advance(time.Minute)
		_, _, err := s.Login(ctx, "admin", password, "attacker")
		if te, ok := IsThrottled(err); !ok || te.RetryIn != p.FailureWindow-time.Minute {
			t.Errorf("blocked client: %v", err)
		}
		if _, _, err := s.Login(ctx, "admin", password, "owner"); err != nil {
			t.Errorf("another client: %v", err)
		}
	})
	t.Run("a success resets the count", func(t *testing.T) {
		s, _, _ := newService(ctx, t)
		for range 3 {
			for range p.MaxFailures - 1 {
				_, _, _ = s.Login(ctx, "admin", "wrong password!!", "c")
			}
			if _, _, err := s.Login(ctx, "admin", password, "c"); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestStoreErrors(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newService(ctx, t)
	token, _, err := s.Login(ctx, "admin", password, "c")
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	st.err = boom
	if _, _, err := s.Login(ctx, "admin", password, "c"); !errors.Is(err, boom) {
		t.Errorf("login: %v", err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, boom) {
		t.Errorf("authenticate: %v", err)
	}
	if err := s.Logout(ctx, Session{}); !errors.Is(err, boom) {
		t.Errorf("logout: %v", err)
	}
	if _, err := s.CreateUser(ctx, "other", "bob", password); !errors.Is(err, boom) {
		t.Errorf("create user: %v", err)
	}
	if err := s.SetPassword(ctx, "admin", password); !errors.Is(err, boom) {
		t.Errorf("set password: %v", err)
	}

	t.Run("corrupt stored hash", func(t *testing.T) {
		s, st, _ := newService(ctx, t)
		for id, u := range st.users {
			u.PasswordHash = "garbage"
			st.users[id] = u
		}
		if _, _, err := s.Login(ctx, "admin", password, "c"); err == nil || errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("login: %v", err)
		}
	})
	t.Run("canceled while waiting to hash", func(t *testing.T) {
		s, _, _ := newService(ctx, t)
		for range cap(s.hashing) {
			s.hashing <- struct{}{}
		}
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, _, err := s.Login(ctx, "admin", password, "c"); !errors.Is(err, context.Canceled) {
			t.Errorf("login: %v", err)
		}
		if _, err := s.CreateUser(ctx, "other", "bob", password); !errors.Is(err, context.Canceled) {
			t.Errorf("create user: %v", err)
		}
	})
	t.Run("random source exhausted", func(t *testing.T) {
		st := newMemStore()
		s := New(st, clock.NewManual(t0), testParams(), bytes.NewReader(make([]byte, saltLen)))
		if _, err := s.CreateUser(ctx, "acc", "admin", password); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Login(ctx, "admin", password, "c"); err == nil {
			t.Error("token read failure ignored")
		}
		if _, _, err := s.Login(ctx, "nobody", password, "c"); err == nil {
			t.Error("decoy failure ignored")
		}
	})
}
