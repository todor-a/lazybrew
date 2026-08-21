package uninstall

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func shortSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "lb-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

type chunkWriter struct {
	buf bytes.Buffer
	max int
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.buf.Write(p)
}

func TestFrameRoundTripWithPartialWrites(t *testing.T) {
	var id RequestID
	copy(id[:], []byte("0123456789abcdef"))
	writer := &chunkWriter{max: 3}
	payload := []byte("password")
	if err := writeFrame(writer, messagePassword, id, payload); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	got, err := readFrame(bytes.NewReader(writer.buf.Bytes()))
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got.typ != messagePassword || got.id != id || !bytes.Equal(got.payload, payload) {
		t.Fatal("frame changed during round trip")
	}
	wipe(got.payload)
}

func TestFrameRejectsMalformedAndBoundaries(t *testing.T) {
	var id RequestID
	valid := func(typ byte, payload []byte) []byte {
		var out bytes.Buffer
		if err := writeFrame(&out, typ, id, payload); err != nil {
			t.Fatalf("write valid frame: %v", err)
		}
		return out.Bytes()
	}

	if _, err := readFrame(bytes.NewReader(valid(messagePassword, make([]byte, maxPasswordBytes)))); err != nil {
		t.Fatalf("1024-byte password rejected: %v", err)
	}
	if err := writeFrame(io.Discard, messagePassword, RequestID{}, make([]byte, maxPasswordBytes+1)); err == nil {
		t.Fatal("oversized password accepted")
	}

	cases := map[string]func([]byte){
		"magic":    func(b []byte) { b[0] = 'X' },
		"version":  func(b []byte) { b[4] = 2 },
		"reserved": func(b []byte) { b[6] = 1 },
		"type":     func(b []byte) { b[5] = 99 },
		"length":   func(b []byte) { binary.BigEndian.PutUint32(b[24:], maxPasswordBytes+1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			encoded := append([]byte(nil), valid(messagePassword, nil)...)
			mutate(encoded)
			if _, err := readFrame(bytes.NewReader(encoded)); err == nil {
				t.Fatal("malformed frame accepted")
			}
		})
	}
	if _, err := readFrame(bytes.NewReader(valid(messageRequest, nil)[:frameSize-1])); err == nil {
		t.Fatal("short frame accepted")
	}
	if err := requireEOF(bytes.NewReader([]byte{1})); err == nil {
		t.Fatal("trailing byte accepted")
	}
}

func TestReadRequestRequiresHalfCloseAndRejectsTrailingBytes(t *testing.T) {
	for _, trailing := range []bool{false, true} {
		name := "missing half-close"
		if trailing {
			name = "trailing byte"
		}
		t.Run(name, func(t *testing.T) {
			path := shortSocketPath(t, "request.sock")
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			server, err := listener.AcceptUnix()
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			if err := writeFrame(client, messageRequest, RequestID{1}, nil); err != nil {
				t.Fatal(err)
			}
			if trailing {
				if err := writeExact(client, []byte{1}); err != nil {
					t.Fatal(err)
				}
				if err := client.CloseWrite(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := readRequest(server); err == nil {
				t.Fatal("incomplete request stream accepted")
			}
		})
	}
}

func TestHelperExchangeRequiresHalfCloseAndResponseEOF(t *testing.T) {
	path := shortSocketPath(t, "protocol.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		id, err := readRequest(conn)
		if err == nil {
			err = writeTerminal(conn, id, messagePassword, []byte("ok"))
		}
		serverDone <- err
	}()

	password, err := helperExchange(path)
	if err != nil {
		t.Fatalf("helperExchange: %v", err)
	}
	defer wipe(password)
	if string(password) != "ok" {
		t.Fatal("unexpected response")
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("broker: %v", err)
	}
}

func TestHelperMetadataIsCanonical(t *testing.T) {
	executable, err := resolvedExecutable()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "lazybrew-aaaaaaaaaaaaaaaaaaaa", "askpass.sock")
	valid := []string{
		sudoAskpassKey + "=" + executable,
		askpassModeKey + "=1",
		askpassSocketKey + "=" + socket,
	}
	if _, ok := validateHelperMetadata(valid); !ok {
		t.Fatal("canonical metadata rejected")
	}
	cases := [][]string{
		append(append([]string(nil), valid...), askpassModeKey+"=1"),
		{sudoAskpassKey + "=" + executable, askpassModeKey + "=0", askpassSocketKey + "=" + socket},
		{sudoAskpassKey + "=/tmp/replaced", askpassModeKey + "=1", askpassSocketKey + "=" + socket},
		{sudoAskpassKey + "=" + executable, askpassModeKey + "=1", askpassSocketKey + "=relative.sock"},
		{sudoAskpassKey + "=" + executable, askpassModeKey + "=1", askpassSocketKey + "=" + filepath.Join(root, "lazybrew-test", "other.sock")},
	}
	for _, env := range cases {
		if _, ok := validateHelperMetadata(env); ok {
			t.Fatal("malformed metadata accepted")
		}
	}
}

func TestCanonicalEnvironmentRemovesEveryRoutingOccurrence(t *testing.T) {
	base := []string{
		"PATH=/bin", "SUDO_ASKPASS=old", "X=1", "SUDO_ASKPASS=older",
		"LAZYBREW_ASKPASS_MODE=0", "LAZYBREW_ASKPASS_SOCKET=/bad",
	}
	got, err := canonicalEnvironment(base, "/resolved/lazybrew", "/private/tmp/lazybrew-a/askpass.sock")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{sudoAskpassKey, askpassModeKey, askpassSocketKey} {
		if countKey(got, key) != 1 {
			t.Fatalf("%s occurs %d times", key, countKey(got, key))
		}
	}
	if countKey(base, sudoAskpassKey) != 2 {
		t.Fatal("input environment was mutated")
	}
}

func TestPrivateEndpointLifecycleIsExactAndIdempotent(t *testing.T) {
	endpoint, err := createEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	dir, socket := endpoint.dirPath, endpoint.socketPath
	if filepath.Base(socket) != "askpass.sock" {
		t.Fatal("socket name is not fixed")
	}
	if err := verifyPath(dir, 0040000, 0700); err != nil {
		t.Fatal(err)
	}
	if err := verifyPath(socket, 0140000, 0600); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.closeExact(); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.closeExact(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("private directory remains")
	}
}

type helperWriterProbe struct {
	calls    int
	seen     []byte
	retained []byte
	n        int
	err      error
}

func (w *helperWriterProbe) Write(p []byte) (int, error) {
	w.calls++
	w.seen = append([]byte(nil), p...)
	w.retained = p
	if w.n >= 0 {
		return w.n, w.err
	}
	return len(p), w.err
}

func TestHelperOutputIsOneAtomicWipedWrite(t *testing.T) {
	password := bytes.Repeat([]byte{'s'}, maxPasswordBytes)
	writer := &helperWriterProbe{n: -1}
	if err := writeHelperOutput(writer, password); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 1 || len(writer.seen) != maxPasswordBytes+1 || writer.seen[len(writer.seen)-1] != '\n' {
		t.Fatalf("writes=%d output-bytes=%d", writer.calls, len(writer.seen))
	}
	if !bytes.Equal(writer.seen[:maxPasswordBytes], password) {
		t.Fatal("stdout password bytes changed")
	}
	for _, value := range writer.retained {
		if value != 0 {
			t.Fatal("password-plus-newline buffer was not wiped")
		}
	}
	for _, value := range password {
		if value != 's' {
			t.Fatal("writeHelperOutput unexpectedly mutated its input")
		}
	}
}

func TestHelperOutputFailureDoesNotRetry(t *testing.T) {
	writer := &helperWriterProbe{n: 0, err: errors.New("stdout failed")}
	if err := writeHelperOutput(writer, []byte("secret")); err == nil {
		t.Fatal("stdout failure was ignored")
	}
	if writer.calls != 1 {
		t.Fatalf("stdout Write called %d times", writer.calls)
	}
	for _, value := range writer.retained {
		if value != 0 {
			t.Fatal("failed output buffer was not wiped")
		}
	}
	oversized := &helperWriterProbe{n: -1}
	if err := writeHelperOutput(oversized, make([]byte, maxPasswordBytes+1)); err == nil {
		t.Fatal("oversized helper output was accepted")
	}
	if oversized.calls != 0 {
		t.Fatal("oversized helper output reached stdout")
	}
}
