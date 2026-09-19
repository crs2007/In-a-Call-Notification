package mqtt

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/eclipse/paho.golang/paho"

	"github.com/crs2007/callmqtt/internal/config"
)

// TestPublishTimeoutConstant pins the 5s bound from TODO 5.3: the engine's
// poll loop can never stall more than one tick-plus-publishTimeout on a
// half-open socket. If this drifts, docs/mqtt.md and the reasoning in
// client.go's comments drift with it, so change it deliberately.
func TestPublishTimeoutConstant(t *testing.T) {
	if publishTimeout != 5*time.Second {
		t.Errorf("publishTimeout = %s, want 5s", publishTimeout)
	}
}

// TestBoundedPublishContextBoundsAnUnboundedCtx is the regression test for
// TODO 5.3: a ctx that would otherwise block forever (context.Background(),
// against a broker that never acks) must come back with a deadline instead
// of none. There's no fake broker in this package to actually hang a publish
// against, so this asserts the thing that makes hanging impossible in the
// first place: publish() never uses the caller's ctx unbounded, it always
// derives a child ctx with a deadline no later than now+publishTimeout.
func TestBoundedPublishContextBoundsAnUnboundedCtx(t *testing.T) {
	before := time.Now()

	ctx, cancel := boundedPublishContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("boundedPublishContext(context.Background()) has no deadline; a stuck PUBACK would hang forever")
	}

	after := time.Now()

	if deadline.Before(before.Add(publishTimeout)) {
		t.Errorf("deadline %s is more than publishTimeout early relative to call start %s", deadline, before)
	}
	if deadline.After(after.Add(publishTimeout)) {
		t.Errorf("deadline %s is later than publishTimeout after call end %s; caller ctx was not bounded", deadline, after)
	}
}

// TestBoundedPublishContextNeverLoosensACallersDeadline confirms the bound is
// additive, not a replacement: a caller that already passed a tighter
// deadline (e.g. a request-scoped ctx) must keep it. publishTimeout should
// only ever shorten an absent or overly generous deadline, never lengthen one
// the caller already set.
func TestBoundedPublishContextNeverLoosensACallersDeadline(t *testing.T) {
	tight := 50 * time.Millisecond
	parent, parentCancel := context.WithTimeout(context.Background(), tight)
	defer parentCancel()

	before := time.Now()
	ctx, cancel := boundedPublishContext(parent)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("boundedPublishContext lost the parent's deadline entirely")
	}
	if deadline.After(before.Add(tight + 10*time.Millisecond)) {
		t.Errorf("deadline %s did not respect the caller's tighter %s deadline", deadline, tight)
	}

	select {
	case <-ctx.Done():
		// Expected: the caller's own tight deadline fired.
	case <-time.After(publishTimeout):
		t.Error("ctx did not expire on the caller's tighter deadline within publishTimeout")
	}
}

// --- 7.1: republish (onConnectionUp's detached body) ------------------------

// fakeConnectionPublisher is a connectionPublisher that records every publish
// instead of talking to a broker — there is no fake broker in this package,
// and *autopaho.ConnectionManager can't be built without dialing one, so this
// is what makes republish (and therefore onConnectionUp's actual logic)
// testable at all.
type fakeConnectionPublisher struct {
	mu sync.Mutex

	published  []*paho.Publish
	calls      int
	failFirstN int // the first N calls fail, to simulate one publish in the chain erroring
}

func (f *fakeConnectionPublisher) Publish(_ context.Context, p *paho.Publish) (*paho.PublishResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failFirstN {
		return nil, errors.New("fake broker: publish failed")
	}
	f.published = append(f.published, p)
	return &paho.PublishResponse{}, nil
}

// newTestClient builds a Client that never dials a broker: its cfg comes from
// the same testConfig used in discovery_test.go, and cm/stateSource are left
// for the test to set up.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	return &Client{cfg: testConfig(t), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// TestRepublishPublishesOnlineDiscoveryThenState is the 7.1 repro: on a
// (re)connect, republish (onConnectionUp's actual body) must publish, in
// order, retained "online", the discovery config, and — because a state
// source is wired — the payload SetStateSource's func returned, verbatim and
// with the same retain setting PublishState itself would use.
func TestRepublishPublishesOnlineDiscoveryThenState(t *testing.T) {
	c := newTestClient(t)
	want := Payload{
		Device:     "sharon-pc",
		State:      "active",
		App:        "teams",
		Apps:       []string{"teams"},
		Confidence: 0.9,
		Network:    "Home",
		Timestamp:  time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC),
	}
	c.SetStateSource(func() (Payload, bool) { return want, true })

	fake := &fakeConnectionPublisher{}
	c.republish(context.Background(), fake)

	if len(fake.published) != 3 {
		t.Fatalf("published %d messages, want 3 (online, discovery, state): %+v", len(fake.published), fake.published)
	}

	avail := fake.published[0]
	if avail.Topic != c.cfg.Topics.Availability || string(avail.Payload) != Online || !avail.Retain {
		t.Errorf("availability publish = %+v, want retained %q on %s", avail, Online, c.cfg.Topics.Availability)
	}

	disc := fake.published[1]
	if disc.Topic != DiscoveryTopic(c.cfg) {
		t.Errorf("discovery publish topic = %q, want %q", disc.Topic, DiscoveryTopic(c.cfg))
	}

	state := fake.published[2]
	if state.Topic != c.cfg.Topics.State {
		t.Errorf("state publish topic = %q, want %q", state.Topic, c.cfg.Topics.State)
	}
	if state.Retain != c.cfg.MQTT.Retain {
		t.Errorf("republished state retain = %v, want %v (must match mqtt.retain)", state.Retain, c.cfg.MQTT.Retain)
	}
	var got Payload
	if err := json.Unmarshal(state.Payload, &got); err != nil {
		t.Fatalf("unmarshal republished state: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("republished payload = %+v, want %+v", got, want)
	}
}

// TestRepublishSkipsStateWhenSourceReportsNotOK covers the ok == false half
// of SetStateSource's contract: nothing has been evaluated yet (e.g. a
// connect that races the engine's first poll), so republish must not invent
// a zero-value Payload to send.
func TestRepublishSkipsStateWhenSourceReportsNotOK(t *testing.T) {
	c := newTestClient(t)
	c.SetStateSource(func() (Payload, bool) { return Payload{}, false })

	fake := &fakeConnectionPublisher{}
	c.republish(context.Background(), fake)

	for _, p := range fake.published {
		if p.Topic == c.cfg.Topics.State {
			t.Errorf("state was published even though the state source reported ok=false: %+v", p)
		}
	}
	if len(fake.published) != 2 {
		t.Errorf("published %d messages, want 2 (online, discovery only): %+v", len(fake.published), fake.published)
	}
}

// TestRepublishSkipsStateWhenNoSourceIsWired covers a Client that never had
// SetStateSource called on it at all — the state it would have gained from
// 7.1 must be a no-op, not a nil-pointer panic.
func TestRepublishSkipsStateWhenNoSourceIsWired(t *testing.T) {
	c := newTestClient(t)

	fake := &fakeConnectionPublisher{}
	c.republish(context.Background(), fake)

	if len(fake.published) != 2 {
		t.Errorf("published %d messages, want 2 (online, discovery only, no state source wired): %+v", len(fake.published), fake.published)
	}
}

// TestRepublishContinuesAfterAvailabilityFailure pins the ordering contract
// in republish's doc comment: a failure on one publish must not skip the
// rest, since discovery and state each still help Home Assistant catch up as
// much as they can from a broker that just forgot everything.
func TestRepublishContinuesAfterAvailabilityFailure(t *testing.T) {
	c := newTestClient(t)
	c.SetStateSource(func() (Payload, bool) { return Payload{State: "active"}, true })

	fake := &fakeConnectionPublisher{failFirstN: 1} // the availability publish fails
	c.republish(context.Background(), fake)

	var sawDiscovery, sawState bool
	for _, p := range fake.published {
		switch p.Topic {
		case DiscoveryTopic(c.cfg):
			sawDiscovery = true
		case c.cfg.Topics.State:
			sawState = true
		}
	}
	if !sawDiscovery || !sawState {
		t.Errorf("a failed availability publish must not skip discovery or state: discovery=%v state=%v", sawDiscovery, sawState)
	}
}

// --- issue 11: buildServerURL -----------------------------------------------

// TestBuildServerURLBracketsIPv6Literals is the regression test for issue
// #11: net.JoinHostPort must bracket an IPv6 literal so the result is
// something net.Dial accepts, rather than the "fd00::1:1883" that
// fmt.Sprintf("%s:%d", ...) used to produce (rejected as "too many colons in
// address").
func TestBuildServerURLBracketsIPv6Literals(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{"ipv6 unbracketed", "fd00::1", "[fd00::1]:1883"},
		{"ipv6 loopback", "::1", "[::1]:1883"},
		{"ipv6 global", "2001:db8::10", "[2001:db8::10]:1883"},
		{"hostname", "broker.local", "broker.local:1883"},
		{"ipv4", "192.168.1.10", "192.168.1.10:1883"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.MQTT.Host = tt.host
			cfg.MQTT.Port = 1883

			got := buildServerURL(cfg)
			if got.Host != tt.want {
				t.Errorf("buildServerURL(%q).Host = %q, want %q", tt.host, got.Host, tt.want)
			}
			if got.Scheme != "mqtt" {
				t.Errorf("buildServerURL(%q).Scheme = %q, want mqtt", tt.host, got.Scheme)
			}
		})
	}
}

// --- 7.5: buildTLSConfig -----------------------------------------------------

// generateSelfSignedCert returns a throwaway self-signed certificate and its
// PEM-encoded key, freshly minted for the calling test. There is no
// checked-in fixture PEM in this repo to reuse, and generating one keeps a
// test from ever depending on a certificate's expiry date.
func generateSelfSignedCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "callmqtt-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestBuildTLSConfigDisabledReturnsNil(t *testing.T) {
	got, err := buildTLSConfig(config.TLS{CAFile: "unused.pem"})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if got != nil {
		t.Errorf("buildTLSConfig with tls.enabled=false = %+v, want nil", got)
	}
}

func TestBuildTLSConfigLoadsCAFile(t *testing.T) {
	certPEM, _ := generateSelfSignedCert(t)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatalf("write ca file: %v", err)
	}

	got, err := buildTLSConfig(config.TLS{Enabled: true, CAFile: caPath})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if got.RootCAs == nil {
		t.Fatal("RootCAs was not populated from ca_file")
	}
	if !got.RootCAs.Equal(mustPool(t, certPEM)) {
		t.Error("RootCAs does not contain the certificate from ca_file")
	}
	if got.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %#x, want TLS 1.2", got.MinVersion)
	}
}

func mustPool(t *testing.T, pemBytes []byte) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatal("test setup: could not build a comparison cert pool")
	}
	return pool
}

func TestBuildTLSConfigRejectsMissingCAFile(t *testing.T) {
	_, err := buildTLSConfig(config.TLS{Enabled: true, CAFile: filepath.Join(t.TempDir(), "missing.pem")})
	if err == nil {
		t.Fatal("expected an error for a ca_file that does not exist")
	}
}

func TestBuildTLSConfigRejectsGarbageCAFile(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, []byte("this is not a pem file"), 0o600); err != nil {
		t.Fatalf("write ca file: %v", err)
	}

	_, err := buildTLSConfig(config.TLS{Enabled: true, CAFile: caPath})
	if err == nil {
		t.Fatal("expected an error for a ca_file with no usable certificates")
	}
}

func TestBuildTLSConfigLoadsClientKeypair(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client-key.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	got, err := buildTLSConfig(config.TLS{Enabled: true, CertFile: certPath, KeyFile: keyPath})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if len(got.Certificates) != 1 {
		t.Fatalf("Certificates = %d entries, want 1", len(got.Certificates))
	}
}

func TestBuildTLSConfigRejectsBadClientKeypair(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client-key.pem")
	if err := os.WriteFile(certPath, []byte("garbage"), 0o600); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("garbage"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	_, err := buildTLSConfig(config.TLS{Enabled: true, CertFile: certPath, KeyFile: keyPath})
	if err == nil {
		t.Fatal("expected an error for an unparsable client keypair")
	}
}

// TestBuildTLSConfigOnlyCertFileSetIsIgnored documents that buildTLSConfig
// itself stays permissive about a lone cert_file/key_file — internal/config's
// Validate is what actually rejects that combination (see config_test.go), so
// this only pins that buildTLSConfig doesn't also need to duplicate the
// check to behave safely if ever called with an unvalidated config.TLS.
func TestBuildTLSConfigOnlyCertFileSetIsIgnored(t *testing.T) {
	certPEM, _ := generateSelfSignedCert(t)
	certPath := filepath.Join(t.TempDir(), "client.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert file: %v", err)
	}

	got, err := buildTLSConfig(config.TLS{Enabled: true, CertFile: certPath})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if len(got.Certificates) != 0 {
		t.Errorf("Certificates = %d entries, want 0 (key_file was never set)", len(got.Certificates))
	}
}

func TestBuildTLSConfigInsecureSkipVerifyPassthrough(t *testing.T) {
	got, err := buildTLSConfig(config.TLS{Enabled: true, InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if !got.InsecureSkipVerify { //nolint:gosec // asserting the pass-through, not weakening anything
		t.Error("InsecureSkipVerify was not passed through from config.TLS")
	}
}
