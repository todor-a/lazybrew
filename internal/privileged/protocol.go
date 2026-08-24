package privileged

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	frameSize        = 28
	maxPasswordBytes = 1024
	maxUnixPath      = 103

	messageRequest  byte = 1
	messagePassword byte = 2
	messageCancel   byte = 3
	messageError    byte = 4
)

var protocolMagic = [4]byte{'L', 'B', 'A', 'P'}
var suffixEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)
var helperOutput io.Writer = os.Stdout

type frame struct {
	typ     byte
	id      RequestID
	payload []byte
}

func writeFrame(w io.Writer, typ byte, id RequestID, payload []byte) error {
	if !validFrame(typ, len(payload)) {
		return errProtocol
	}
	if typ == messagePassword && !utf8.Valid(payload) {
		return errProtocol
	}
	header := make([]byte, frameSize)
	defer wipe(header)
	copy(header[:4], protocolMagic[:])
	header[4] = 1
	header[5] = typ
	copy(header[8:24], id[:])
	binary.BigEndian.PutUint32(header[24:], uint32(len(payload)))
	if err := writeExact(w, header); err != nil {
		return errProtocol
	}
	if err := writeExact(w, payload); err != nil {
		return errProtocol
	}
	return nil
}

func readFrame(r io.Reader) (frame, error) {
	header := make([]byte, frameSize)
	defer wipe(header)
	if _, err := io.ReadFull(r, header); err != nil {
		return frame{}, errProtocol
	}
	if !bytes.Equal(header[:4], protocolMagic[:]) || header[4] != 1 || header[6] != 0 || header[7] != 0 {
		return frame{}, errProtocol
	}
	typ := header[5]
	length := int(binary.BigEndian.Uint32(header[24:]))
	if !validFrame(typ, length) {
		return frame{}, errProtocol
	}
	var id RequestID
	copy(id[:], header[8:24])
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		wipe(payload)
		return frame{}, errProtocol
	}
	if typ == messagePassword && !utf8.Valid(payload) {
		wipe(payload)
		return frame{}, errProtocol
	}
	return frame{typ: typ, id: id, payload: payload}, nil
}

func validFrame(typ byte, length int) bool {
	switch typ {
	case messageRequest, messageCancel, messageError:
		return length == 0
	case messagePassword:
		return length >= 0 && length <= maxPasswordBytes
	default:
		return false
	}
}

func writeExact(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func readRequest(conn *net.UnixConn) (RequestID, error) {
	if err := conn.SetDeadline(time.Now().Add(protocolTimeout)); err != nil {
		return RequestID{}, errProtocol
	}
	f, err := readFrame(conn)
	if err != nil || f.typ != messageRequest || len(f.payload) != 0 {
		wipe(f.payload)
		return RequestID{}, errProtocol
	}
	if err := conn.SetReadDeadline(time.Now().Add(protocolTimeout)); err != nil {
		return RequestID{}, errProtocol
	}
	if err := requireEOF(conn); err != nil {
		return RequestID{}, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return RequestID{}, errProtocol
	}
	return f.id, nil
}

func writeTerminal(conn *net.UnixConn, id RequestID, typ byte, payload []byte) error {
	if typ != messagePassword && typ != messageCancel && typ != messageError {
		return errProtocol
	}
	if err := conn.SetWriteDeadline(time.Now().Add(protocolTimeout)); err != nil {
		return errProtocol
	}
	if err := writeFrame(conn, typ, id, payload); err != nil {
		return err
	}
	return nil
}

func requireEOF(r io.Reader) error {
	var extra [1]byte
	n, err := r.Read(extra[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return errProtocol
	}
	return nil
}

func helperExchange(socketPath string) ([]byte, error) {
	address := &net.UnixAddr{Name: socketPath, Net: "unix"}
	dialer := net.Dialer{Timeout: protocolTimeout}
	raw, err := dialer.Dial("unix", address.String())
	if err != nil {
		return nil, errProtocol
	}
	conn, ok := raw.(*net.UnixConn)
	if !ok {
		_ = raw.Close()
		return nil, errProtocol
	}
	defer conn.Close()

	var id RequestID
	if _, err := rand.Read(id[:]); err != nil {
		return nil, errProtocol
	}
	if err := conn.SetWriteDeadline(time.Now().Add(protocolTimeout)); err != nil {
		return nil, errProtocol
	}
	if err := writeFrame(conn, messageRequest, id, nil); err != nil {
		return nil, err
	}
	if err := conn.CloseWrite(); err != nil {
		return nil, errProtocol
	}
	if err := conn.SetReadDeadline(time.Now().Add(passwordTimeout)); err != nil {
		return nil, errProtocol
	}
	f, err := readFrame(conn)
	if err != nil {
		return nil, err
	}
	if f.id != id || (f.typ != messagePassword && f.typ != messageCancel && f.typ != messageError) {
		wipe(f.payload)
		return nil, errProtocol
	}
	if err := conn.SetReadDeadline(time.Now().Add(protocolTimeout)); err != nil {
		wipe(f.payload)
		return nil, errProtocol
	}
	if err := requireEOF(conn); err != nil {
		wipe(f.payload)
		return nil, err
	}
	if f.typ != messagePassword {
		wipe(f.payload)
		return nil, errProtocol
	}
	return f.payload, nil
}

// RunHelperFromEnv detects raw helper mode before normal startup. Helper mode is
// deliberately silent; the returned code is the complete process result.
func RunHelperFromEnv() (handled bool, exitCode int) {
	env := os.Environ()
	if countKey(env, askpassModeKey) == 0 {
		// brew's bin/brew sanitizes the environment of everything it runs:
		// SUDO_ASKPASS survives its whitelist but the LAZYBREW_* markers do
		// not, so when brew's own sudo execs this binary the environment
		// carries no helper evidence at all. The invocation path is the one
		// channel brew cannot strip — SUDO_ASKPASS names the per-job helper
		// link and sudo passes that exact string through as argv[0].
		socket, isHelper, valid := helperSocketFromInvocation(os.Args)
		if !isHelper {
			return false, 0
		}
		// A process running under the helper name is never a legitimate
		// interactive run, so an invalid claim fails closed instead of
		// falling through to normal startup and printing the TTY refusal
		// into sudo's askpass pipe.
		if setCoreLimit() != nil || !valid {
			return true, 1
		}
		return true, helperExit(socket)
	}
	if setCoreLimit() != nil {
		return true, 1
	}
	metadata, ok := validateHelperMetadata(env)
	if !ok {
		return true, 1
	}
	return true, helperExit(metadata.socket)
}

// helperExit is the shared helper tail — one exchange, one write, everything
// wiped — so the two detection routes cannot drift apart.
func helperExit(socketPath string) int {
	password, err := helperExchange(socketPath)
	if err != nil {
		return 1
	}
	defer wipe(password)
	if err := writeHelperOutput(helperOutput, password); err != nil {
		return 1
	}
	return 0
}

// helperLinkName is the fixed basename of the per-job SUDO_ASKPASS symlink.
// The name doubles as the helper-mode declaration: sudo passes the
// SUDO_ASKPASS string through as argv[0], and no legitimate interactive run
// of lazybrew is ever spelled this way.
const helperLinkName = "lazybrew-askpass"

// helperSocketFromInvocation reads helper mode out of argv when the
// environment carries no markers. isHelper reports that argv[0] claims the
// helper name at all; valid reports that the claim survives the same
// canonical-shape checks the environment route applies, plus proof that the
// link resolves to this very binary.
//
// SECURITY: argv[0] is attacker-influenceable — anyone with a shell can exec
// this binary under the helper name above a crafted directory. That gains
// nothing: the helper holds no secret of its own, and a password only ever
// leaves the app after acquirePeerEvidence has verified the connecting peer
// as a same-uid descendant of the job's own brew child carrying this
// binary's code identity — which a process the attacker started is not.
// Reaching a real job's socket also requires traversing its 0700 directory,
// same-uid access that could already dial the socket directly, so the helper
// adds no capability. The EvalSymlinks comparison additionally refuses to
// act as askpass on behalf of any other binary.
func helperSocketFromInvocation(args []string) (socket string, isHelper, valid bool) {
	if len(args) == 0 || filepath.Base(args[0]) != helperLinkName {
		return "", false, false
	}
	argv0 := args[0]
	if !filepath.IsAbs(argv0) || filepath.Clean(argv0) != argv0 {
		return "", true, false
	}
	dir := filepath.Dir(argv0)
	tmpRoot, err := filepath.EvalSymlinks("/tmp")
	if err != nil || filepath.Dir(dir) != tmpRoot || !validPrivateDirName(filepath.Base(dir)) {
		return "", true, false
	}
	// The lexical checks above cannot tell a real private directory from a
	// same-named symlink or a loosened one; require the same structure the
	// app verified when it created the endpoint. Defense in depth only - a
	// same-uid attacker can build a compliant directory of their own, and
	// the password stays guarded by the app-side peer verification either
	// way.
	if verifyPath(dir, syscall.S_IFDIR, 0o700) != nil {
		return "", true, false
	}
	resolved, err := filepath.EvalSymlinks(argv0)
	if err != nil {
		return "", true, false
	}
	actual, err := resolvedExecutable()
	if err != nil || resolved != actual {
		return "", true, false
	}
	socket = filepath.Join(dir, "askpass.sock")
	if len([]byte(socket)) > maxUnixPath {
		return "", true, false
	}
	return socket, true, true
}

func writeHelperOutput(w io.Writer, password []byte) error {
	if len(password) > maxPasswordBytes {
		return errProtocol
	}
	output := make([]byte, len(password)+1)
	copy(output, password)
	output[len(password)] = '\n'
	defer wipe(output)
	n, err := w.Write(output)
	if err != nil || n != len(output) {
		return errProtocol
	}
	return nil
}

type helperMetadata struct {
	executable string
	socket     string
}

func validateHelperMetadata(env []string) (helperMetadata, bool) {
	if countKey(env, askpassModeKey) != 1 || countKey(env, askpassSocketKey) != 1 || countKey(env, sudoAskpassKey) != 1 {
		return helperMetadata{}, false
	}
	if rawValue(env, askpassModeKey) != "1" {
		return helperMetadata{}, false
	}
	executable := rawValue(env, sudoAskpassKey)
	socketPath := rawValue(env, askpassSocketKey)
	if executable == "" || socketPath == "" || !filepath.IsAbs(executable) || !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath || len([]byte(socketPath)) > maxUnixPath {
		return helperMetadata{}, false
	}
	// SUDO_ASKPASS names the per-job helper link rather than the binary, so
	// the comparison resolves it first; a direct executable path keeps
	// passing because it resolves to itself.
	resolvedAskpass, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return helperMetadata{}, false
	}
	actual, err := resolvedExecutable()
	if err != nil || actual != resolvedAskpass {
		return helperMetadata{}, false
	}
	tmpRoot, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return helperMetadata{}, false
	}
	dir := filepath.Dir(socketPath)
	if filepath.Base(socketPath) != "askpass.sock" || filepath.Dir(dir) != tmpRoot || !validPrivateDirName(filepath.Base(dir)) {
		return helperMetadata{}, false
	}
	return helperMetadata{executable: executable, socket: socketPath}, true
}
func validPrivateDirName(name string) bool {
	const prefix = "lazybrew-"
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+20 {
		return false
	}
	for _, char := range name[len(prefix):] {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz234567", char) {
			return false
		}
	}
	return true
}

func countKey(env []string, key string) int {
	count := 0
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) || entry == key {
			count++
		}
	}
	return count
}

func rawValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

type privateEndpoint struct {
	rootPath   string
	dirPath    string
	socketPath string
	helperPath string
	listener   *net.UnixListener
	closeOnce  sync.Once
	closeErr   error
}

// installHelperLink creates the per-job SUDO_ASKPASS symlink next to the
// socket, inside the directory createEndpoint has already verified as a
// 0700-owned non-symlink. It is the socket's path-detection twin: fixed
// name, private directory, removed by closeExact before the directory
// itself so the directory removal stays a plain empty-dir Remove.
func (p *privateEndpoint) installHelperLink(executable string) (string, error) {
	if executable == "" || !filepath.IsAbs(executable) {
		return "", errors.New("invalid askpass routing metadata")
	}
	path := filepath.Join(p.dirPath, helperLinkName)
	// Recorded before the syscall: os.Symlink refuses to clobber an existing
	// entry, and on that failure the squatted file must still be removed by
	// closeExact or the directory removal stops being a plain empty-dir
	// Remove and the private directory outlives the job.
	p.helperPath = path
	if err := os.Symlink(executable, path); err != nil {
		return "", err
	}
	return path, nil
}

func createEndpoint() (_ *privateEndpoint, retErr error) {
	root, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return nil, errors.New("resolved /tmp is not a directory")
	}

	var dir string
	for range 32 {
		var suffix [12]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, err
		}
		name := "lazybrew-" + suffixEncoding.EncodeToString(suffix[:])
		dir = filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err == nil {
			break
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		dir = ""
	}
	if dir == "" {
		return nil, errors.New("could not allocate private directory")
	}
	ep := &privateEndpoint{rootPath: root, dirPath: dir, socketPath: filepath.Join(dir, "askpass.sock")}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, ep.closeExact())
		}
	}()
	if err := verifyPath(dir, syscall.S_IFDIR, 0700); err != nil {
		return nil, err
	}
	if len([]byte(ep.socketPath)) > maxUnixPath {
		return nil, errors.New("askpass socket path is too long")
	}
	listener, err := listenUnixBacklogOne(ep.socketPath)
	if err != nil {
		return nil, err
	}
	ep.listener = listener
	if err := os.Chmod(ep.socketPath, 0600); err != nil {
		return nil, err
	}
	if err := verifyPath(ep.socketPath, syscall.S_IFSOCK, 0600); err != nil {
		return nil, err
	}
	return ep, nil
}

func (p *privateEndpoint) closeExact() error {
	p.closeOnce.Do(func() {
		var errs []error
		if p.listener != nil {
			if err := p.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, err)
			}
		}
		if p.socketPath != "" {
			if err := os.Remove(p.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		if p.helperPath != "" {
			if err := os.Remove(p.helperPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		if p.dirPath != "" {
			if err := os.Remove(p.dirPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		p.closeErr = errors.Join(errs...)
	})
	return p.closeErr
}

func listenUnixBacklogOne(path string) (*net.UnixListener, error) {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	owned := true
	defer func() {
		if owned {
			_ = syscall.Close(fd)
		}
	}()
	syscall.CloseOnExec(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		return nil, err
	}
	if err := syscall.Listen(fd, 1); err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "lazybrew-askpass-listener")
	if file == nil {
		return nil, errors.New("could not own listener descriptor")
	}
	owned = false
	listener, err := net.FileListener(file)
	_ = file.Close()
	if err != nil {
		return nil, err
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		_ = listener.Close()
		return nil, errors.New("unexpected listener type")
	}
	return unixListener, nil
}

func verifyPath(path string, kind uint32, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || uint32(stat.Mode)&syscall.S_IFMT != kind || info.Mode().Perm() != mode || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private askpass path failed verification")
	}
	return nil
}

var errProtocol = errors.New("askpass protocol failed")
