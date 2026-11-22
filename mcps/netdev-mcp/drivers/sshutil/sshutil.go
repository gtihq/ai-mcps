// Package sshutil contains the SSH session helper shared by the RouterOS and
// generic CLI drivers.
package sshutil

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
	"golang.org/x/crypto/ssh"
)

// Exec runs one command over SSH and returns its combined output. The session
// is closed when the context is cancelled, which keeps a stalled device from
// hanging an MCP call forever.
func Exec(ctx context.Context, cfg device.Config, timeout time.Duration, command string) (string, error) {
	methods, err := AuthMethods(cfg)
	if err != nil {
		return "", err
	}
	if len(methods) == 0 {
		return "", fmt.Errorf("ssh: no authentication configured for %s", cfg.Name)
	}
	clientConfig := &ssh.ClientConfig{
		User:            device.StringOr(cfg.Username, "admin"),
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	client, err := ssh.Dial("tcp", cfg.Address(22), clientConfig)
	if err != nil {
		return "", fmt.Errorf("ssh: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		session, err := client.NewSession()
		if err != nil {
			done <- result{err: err}
			return
		}
		defer func() {
			_ = session.Close()
		}()
		out, err := session.CombinedOutput(command)
		done <- result{out: out, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		text := strings.TrimRight(string(r.out), "\r\n")
		if r.err != nil && text == "" {
			return "", fmt.Errorf("ssh: %w", r.err)
		}
		return text, nil
	}
}

// AuthMethods builds password and private-key auth methods from the inventory.
func AuthMethods(cfg device.Config) ([]ssh.AuthMethod, error) {
	var pemBytes []byte
	if raw, ok := cfg.Extra["private_key"]; ok && device.MapString(raw) != "" {
		pemBytes = []byte(device.MapString(raw))
	}
	if path, ok := cfg.Extra["private_key_path"]; ok && device.MapString(path) != "" {
		data, err := os.ReadFile(device.MapString(path))
		if err != nil {
			return nil, fmt.Errorf("ssh: read private key: %w", err)
		}
		pemBytes = data
	}
	methods := make([]ssh.AuthMethod, 0, 2)
	if len(pemBytes) > 0 {
		signer, err := ssh.ParsePrivateKey(pemBytes)
		if err != nil {
			return nil, fmt.Errorf("ssh: parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}
	return methods, nil
}
