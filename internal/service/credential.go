package service

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os/exec"
	"strings"
)

const (
	passwordLength  = 16
	passwordCharset = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
	defaultUsername  = "forgemill"
)

// generatePassword creates a random password from an unambiguous character set.
func generatePassword() (string, error) {
	b := make([]byte, passwordLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordCharset))))
		if err != nil {
			return "", fmt.Errorf("generate random byte: %w", err)
		}
		b[i] = passwordCharset[n.Int64()]
	}
	return string(b), nil
}

// hashPasswordSHA512 creates a SHA-512 crypt ($6$) hash using openssl.
func hashPasswordSHA512(password string) (string, error) {
	cmd := exec.Command("openssl", "passwd", "-6", "-stdin")
	cmd.Stdin = strings.NewReader(password)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("openssl passwd: %w", err)
	}
	hash := strings.TrimSpace(string(out))
	if !strings.HasPrefix(hash, "$6$") {
		return "", fmt.Errorf("unexpected hash format: %s", hash)
	}
	return hash, nil
}

// ISSUE-01: Removed duplicate buildCloudInitUserdata function.
// The canonical copy lives in internal/provider/vmware/deploy.go.
// Each provider handles cloud-init injection independently via DeploySpec.
