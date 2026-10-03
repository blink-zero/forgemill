package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fakeSSHD is the smallest SSH server that lets sshExecute run end to end:
// password auth accepts anything, every "exec" request is recorded, the first
// one behaves per `firstMode` ("hang" never exits, "ok" prints hello and
// exits 0) and every later one (the cancel path's kill command) exits 0.
type fakeSSHD struct {
	addr      string
	firstMode string
	mu        sync.Mutex
	execs     []string
	closed    chan struct{}
}

func startFakeSSHD(t *testing.T, firstMode string) *fakeSSHD {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSSHD{addr: ln.Addr().String(), firstMode: firstMode, closed: make(chan struct{})}
	t.Cleanup(func() { close(f.closed); ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c, cfg)
		}
	}()
	return f
}

func (f *fakeSSHD) serve(c net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			defer ch.Close()
			for r := range chReqs {
				if r.Type != "exec" {
					if r.WantReply {
						r.Reply(false, nil)
					}
					continue
				}
				cmd := string(r.Payload[4:])
				f.mu.Lock()
				f.execs = append(f.execs, cmd)
				n := len(f.execs)
				f.mu.Unlock()
				r.Reply(true, nil)
				if n == 1 && f.firstMode == "hang" {
					<-f.closed // never exits on its own
					return
				}
				fmt.Fprintln(ch, "hello")
				ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				return
			}
		}()
	}
}

func (f *fakeSSHD) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.execs...)
}

func hostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	fmt.Sscanf(p, "%d", &port)
	return h, port
}

func TestSSHExecuteRunsScriptUnderExecutionMarkerAndStreamsOutput(t *testing.T) {
	srv := startFakeSSHD(t, "ok")
	host, port := hostPort(t, srv.addr)
	var lines []string
	code, err := sshExecute(context.Background(), host, port, "u", "p", "echo hi", "", func(l string) { lines = append(lines, l) }, nil, 0, 42)
	if err != nil || code != 0 {
		t.Fatalf("exit=%d err=%v", code, err)
	}
	if len(lines) != 1 || lines[0] != "hello" {
		t.Errorf("output not streamed: %v", lines)
	}
	cmds := srv.commands()
	if len(cmds) != 1 || !strings.HasPrefix(cmds[0], "sudo bash -c 'exec -a forgemill-exec-42 bash' <<'FORGEMILL_SCRIPT'") {
		t.Errorf("job must run under its execution marker, got %q", cmds)
	}
	if !strings.Contains(cmds[0], "set -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\necho hi\nFORGEMILL_SCRIPT") {
		t.Errorf("script preamble/body changed: %q", cmds[0])
	}
}

func TestSSHExecuteCancelKillsRemoteJobAndReturnsPromptly(t *testing.T) {
	srv := startFakeSSHD(t, "hang")
	host, port := hostPort(t, srv.addr)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()

	start := time.Now()
	_, err := sshExecute(ctx, host, port, "u", "p", "sleep 300", "", func(string) {}, nil, 0, 7)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled so the execution is marked cancelled, got %v", err)
	}
	// Before the fix this blocked until the remote script ended on its own.
	if elapsed > 15*time.Second {
		t.Errorf("cancel took %s; must return within the bounded kill/teardown window", elapsed)
	}
	cmds := srv.commands()
	if len(cmds) != 2 {
		t.Fatalf("expected the job plus one kill command, got %d: %q", len(cmds), cmds)
	}
	kill := cmds[1]
	if !strings.Contains(kill, `pgrep -n -f "^forgemill-exec-7$"`) || !strings.Contains(kill, `pkill -TERM -g "$pgid"`) || !strings.Contains(kill, `pkill -KILL -g "$pgid"`) {
		t.Errorf("kill command must target the job's process group by anchored marker: %q", kill)
	}
	if !strings.HasPrefix(kill, "sudo sh -c '") {
		t.Errorf("kill must run as root on the guest: %q", kill)
	}
}

func TestRemoteShellWithoutExecutionIDIsPlainBash(t *testing.T) {
	if remoteShell(0) != "bash" {
		t.Errorf("legacy callers must keep plain bash, got %q", remoteShell(0))
	}
}

// The kill script must take down the whole job — the marker bash *and* its
// children — under /bin/sh, which is dash on Debian/Ubuntu guests. This runs
// it for real against a local process group shaped like the remote job.
func TestKillJobScriptTerminatesMarkerBashAndItsChildren(t *testing.T) {
	for _, bin := range []string{"sh", "bash", "pgrep", "pkill", "ps"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}
	marker := fmt.Sprintf("forgemill-exec-test-%d", os.Getpid())
	job := exec.Command("bash", "-c", "bash -c 'exec -a "+marker+" bash' <<'EOF'\nset -euo pipefail\nsleep 300\nEOF")
	job.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // own session/group, like sshd+sudo give it
	if err := job.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-job.Process.Pid, syscall.SIGKILL) })

	// Wait for the marker bash and its sleep child to exist.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if out, _ := exec.Command("pgrep", "-f", "^"+marker+"$").Output(); len(out) > 0 {
			if out, _ := exec.Command("pgrep", "-g", fmt.Sprint(job.Process.Pid), "-x", "sleep").Output(); len(out) > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if out, err := exec.Command("sh", "-c", killJobScript(marker)).CombinedOutput(); err != nil {
		t.Fatalf("kill script failed under sh: %v: %s", err, out)
	}
	time.Sleep(200 * time.Millisecond)
	if out, _ := exec.Command("pgrep", "-f", "^"+marker+"$").Output(); len(out) > 0 {
		t.Errorf("marker bash survived: pids %s", out)
	}
	if out, _ := exec.Command("pgrep", "-g", fmt.Sprint(job.Process.Pid), "-x", "sleep").Output(); len(out) > 0 {
		t.Errorf("child sleep survived the process-group kill: pids %s", out)
	}
}
