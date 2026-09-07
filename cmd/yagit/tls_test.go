package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The HTTPS half of the daemon, which nothing else reaches. The end-to-end
// suite drives a real process, and that process serves plain HTTP; every
// decision TLS changes — the certificate, the accepted origins, the Secure
// attribute on the session cookie — would otherwise go unexercised.
//
// The certificate is generated here rather than committed. A fixture pair has
// an expiry date, and the day it passes is the day these tests start failing
// on a change that had nothing to do with them.

const (
	// tlsTestWait bounds every wait in this file. It is generous on purpose:
	// it exists to turn a hang into a failure with a message, not to measure
	// anything.
	tlsTestWait = 10 * time.Second

	// signalRetry paces the SIGTERM below. Short enough that a normal run
	// sends one signal and stops.
	signalRetry = 50 * time.Millisecond
)

// TestServeUntilSignalTLSPresentsThePairItWasGiven covers the serving half:
// the two files load, ServeTLS answers on the listener it was handed with the
// certificate it was configured with, and the ErrServerClosed that a shutdown
// produces comes back as success. That last one is the trap — reported as an
// error, every clean stop would end in exit(1).
func TestServeUntilSignalTLSPresentsThePairItWasGiven(t *testing.T) {
	certFile, keyFile := selfSignedPair(t)

	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}),
	}

	// Listening here rather than inside the call under test is what removes
	// the poll loop: the socket is bound before the first request is made, so
	// a connection arriving before ServeTLS accepts waits in the kernel's
	// backlog instead of being refused.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	stopped := make(chan error, 1)
	go func() {
		stopped <- serveUntilSignalTLS(server, listener, certFile, keyFile, slog.New(slog.DiscardHandler))
	}()

	client := httpsClient(t, certFile)
	response, err := client.Get("https://" + listener.Addr().String() + "/")
	if err != nil {
		// A pair that did not load ends the goroutine instead of answering,
		// and its error says which of the two files was wrong.
		select {
		case serveError := <-stopped:
			t.Fatalf("the server stopped instead of serving: %v", serveError)
		default:
		}
		t.Fatalf("HTTPS GET: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", response.StatusCode)
	}
	if response.TLS == nil {
		t.Fatal("the response did not come over TLS")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("serveUntilSignalTLS = %v, expected a clean stop to read as success", err)
		}
	case <-time.After(tlsTestWait):
		t.Fatal("serveUntilSignalTLS never returned after Shutdown")
	}
}

// TestServeUntilSignalTLSReportsAPairItCannotLoad covers the other ending. A
// certificate that is missing, unreadable or not a certificate at all is the
// most likely thing to be wrong on the machine where TLS is switched on for
// the first time, and the daemon has to say so rather than exit quietly.
func TestServeUntilSignalTLSReportsAPairItCannotLoad(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "there-is-no-certificate-here.pem")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	// ServeTLS gives up before it wraps the listener when the pair does not
	// load, so nothing downstream will ever close it.
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("closing the listener: %v", err)
		}
	}()

	err = serveUntilSignalTLS(&http.Server{}, listener, missing, missing, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("a certificate that does not exist must stop the daemon")
	}
	// Both halves of the message: which service failed, and which file it was
	// looking for. "no such file or directory" on its own leaves the reader
	// guessing between the certificate and the key.
	if !strings.Contains(err.Error(), "HTTPS service") {
		t.Errorf("the error does not say which service failed: %v", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the error does not name the file: %v", err)
	}
}

// TestServingHTTPSChangesTheCookieAndTheAcceptedOrigins runs the daemon the
// way main() does and asks it for a session over a real TLS connection.
//
// It is the only place where the three decisions the scheme drives are checked
// together and in the wiring rather than one layer down: the cookie's Secure
// attribute, the origins a browser may present, and the shutdown at the end of
// serveUntilSignalTLS. Get the first one backwards and nothing crashes — the
// browser simply stops sending the cookie back, and every session fails to
// establish on the one deployment nobody can reproduce locally.
func TestServingHTTPSChangesTheCookieAndTheAcceptedOrigins(t *testing.T) {
	if runtime.GOOS == "windows" {
		// os.Process.Signal delivers nothing but Kill there, and Kill takes
		// the test binary with it, so the graceful stop this test ends on
		// cannot be asked for. What it asserts is covered one layer down on
		// every platform: the cookie in internal/api/auth_test.go, the
		// origins and the pairing rule in main_test.go, ServeTLS above.
		t.Skip("a SIGTERM cannot be delivered inside the test process on Windows")
	}

	const token = "a-token-long-enough-to-count-as-one"

	certFile, keyFile := selfSignedPair(t)

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}

	// The embedded frontend exists only after `./do build`, so a test needing
	// it would pass on a machine that had built and fail on one that had not.
	// The daemon proxies to this stand-in instead; nothing under /api goes
	// anywhere near it.
	frontend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer frontend.Close()

	t.Setenv("YAGIT_TOKEN", token)

	// Registered before the daemon starts, and this is what keeps the test
	// binary alive: SIGTERM ends a process that has no handler for it, and the
	// daemon installs its own only after it has begun serving.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	started := make(chan string, 1)
	stopped := make(chan error, 1)
	go func() {
		stopped <- run(configuration{
			addr:              "127.0.0.1:0",
			publicHost:        "127.0.0.1",
			root:              root,
			tlsCert:           certFile,
			tlsKey:            keyFile,
			frontendDevServer: frontend.URL,
		}, slog.New(&announcedAddress{
			// A handler that discards its output rather than
			// slog.DiscardHandler: that one answers Enabled with false, so
			// Handle is never called and there would be nothing to read the
			// address from.
			Handler: slog.NewTextHandler(io.Discard, nil),
			addr:    started,
		}))
	}()

	var address string
	select {
	case address = <-started:
	case err := <-stopped:
		t.Fatalf("the daemon stopped before it served anything: %v", err)
	case <-time.After(tlsTestWait):
		t.Fatal("the daemon never announced an address")
	}

	client := httpsClient(t, certFile)
	cookie := exchangeTokenForACookie(t, client, "https://"+address, token)

	// The assertion the whole file exists for.
	if !cookie.Secure {
		t.Error("the session cookie has no Secure attribute although the daemon serves HTTPS")
	}

	assertOriginRules(t, client, address, cookie)

	// What is being measured below is the shutdown, not the grace period a
	// client still holding a connection is entitled to. A pooled keep-alive
	// connection left open makes Shutdown wait out shutdownGracePeriod and
	// then report a deadline, which is correct behaviour and a five-second
	// answer to a question this test is not asking.
	client.CloseIdleConnections()

	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("finding this process: %v", err)
	}

	// Sent more than once on purpose. The daemon serves before it installs its
	// handler, so a signal delivered in that gap is seen by the guard above
	// and by nobody else; repeating costs a few milliseconds and removes a
	// flake that would only ever appear on a loaded CI machine.
	deadline := time.Now().Add(tlsTestWait)
	for {
		if err := self.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("sending SIGTERM to this process: %v", err)
		}
		select {
		case err := <-stopped:
			if err != nil {
				t.Fatalf("the daemon stopped on an error rather than on the signal: %v", err)
			}
			return
		case <-time.After(signalRetry):
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon did not stop on SIGTERM")
		}
	}
}

// exchangeTokenForACookie does what a browser without JavaScript does: POST
// the token to /api/session and keep the cookie that comes back.
func exchangeTokenForACookie(t *testing.T, client *http.Client, base, token string) *http.Cookie {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, base+"/api/session",
		strings.NewReader(`{"token":"`+token+`"}`))
	if err != nil {
		t.Fatalf("building the session request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST /api/session over TLS: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}

	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(cookies))
	}
	return cookies[0]
}

// assertOriginRules checks which origins a cookie-authenticated mutating
// request may present. Under TLS they are the https spellings and only those:
// the same host and port over plain http is a different origin, and one the
// daemon answers nothing on.
func assertOriginRules(t *testing.T, client *http.Client, address string, cookie *http.Cookie) {
	t.Helper()

	cases := []struct {
		name   string
		origin string
		want   int
	}{
		// 400 rather than 200: the body names no repository, so the request
		// dies further along. What matters is that it got past the origin.
		{"the https origin the daemon announced", "https://" + address, http.StatusBadRequest},
		{"the same address over plain http", "http://" + address, http.StatusForbidden},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost,
				"https://"+address+"/api/repos", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("building the request: %v", err)
			}
			request.AddCookie(cookie)
			request.Header.Set("Origin", testCase.origin)

			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("POST /api/repos over TLS: %v", err)
			}
			defer func() { _ = response.Body.Close() }()
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Fatalf("reading the answer: %v", err)
			}

			if response.StatusCode != testCase.want {
				t.Errorf("status = %d, want %d", response.StatusCode, testCase.want)
			}
		})
	}
}

// announcedAddress reads the bound address out of the daemon's own startup
// line.
//
// The daemon is asked for port 0 so that concurrent tests never fight over a
// port, and the port the kernel picked is published in exactly one place —
// the line an operator reads to find out where yagit is listening. Matching
// it by text pins that line too, which is no loss: an announcement nobody can
// parse is an announcement nobody can use.
type announcedAddress struct {
	slog.Handler
	addr chan<- string
}

func (h *announcedAddress) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "yagit daemon started" {
		record.Attrs(func(attribute slog.Attr) bool {
			if attribute.Key != "addr" {
				return true
			}
			// Non-blocking: a log handler that waits for a reader would hang
			// the daemon it is supposed to be watching.
			select {
			case h.addr <- attribute.Value.String():
			default:
			}
			return false
		})
	}
	return h.Handler.Handle(ctx, record)
}

// httpsClient trusts exactly one certificate: the one on disk at certFile.
//
// A pool rather than InsecureSkipVerify, because skipping verification would
// let these tests pass against any certificate at all — including one the
// daemon was never given, which is precisely the failure they are here to
// catch.
func httpsClient(t *testing.T, certFile string) *http.Client {
	t.Helper()

	encoded, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("reading the certificate back: %v", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(encoded) {
		t.Fatalf("%s does not hold a PEM certificate", certFile)
	}

	return &http.Client{
		Timeout: tlsTestWait,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}

// selfSignedPair writes a certificate and its key into the test's own
// temporary directory and returns the two paths.
func selfSignedPair(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating the key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generating the serial number: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "yagit test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		// The tests dial the loopback by address, so what a verifier checks is
		// an IP rather than a name. A certificate carrying only a common name
		// is refused by everything written this century, Go included.
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating the certificate: %v", err)
	}

	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("encoding the key: %v", err)
	}

	directory := t.TempDir()
	certFile = filepath.Join(directory, "cert.pem")
	keyFile = filepath.Join(directory, "key.pem")

	writePEM(t, certFile, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	writePEM(t, keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})

	return certFile, keyFile
}

func writePEM(t *testing.T, path string, block *pem.Block) {
	t.Helper()

	encoded := pem.EncodeToMemory(block)
	if encoded == nil {
		t.Fatalf("encoding %s as PEM", path)
	}
	// 0600 for the key, and the same for the certificate beside it: this is
	// what the file mode of a real pair looks like, and a test that wrote a
	// world-readable private key would be teaching the wrong habit.
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
