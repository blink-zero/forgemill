package service

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	ldapv3 "github.com/go-ldap/ldap/v3"

	"github.com/forgemill/forgemill/internal/crypto"
	"github.com/forgemill/forgemill/internal/db"
	"github.com/forgemill/forgemill/internal/db/models"
)

type LDAPService struct {
	db  *db.DB
	enc *crypto.Encryptor
}

func NewLDAPService(db *db.DB, enc *crypto.Encryptor) *LDAPService {
	return &LDAPService{db: db, enc: enc}
}

type LDAPConfig struct {
	Server                string            `json:"server"`
	Port                  int               `json:"port"`
	UseTLS                bool              `json:"use_tls"`
	BindDN                string            `json:"bind_dn"`
	BindPasswordEncrypted string            `json:"bind_password_encrypted"`
	SearchBase            string            `json:"search_base"`
	SearchFilter          string            `json:"search_filter"`
	GroupSearchBase       string            `json:"group_search_base"`
	AdminGroup            string            `json:"admin_group"`
	UserGroup             string            `json:"user_group"`
	Attributes            LDAPAttributes    `json:"attributes"`
}

type LDAPAttributes struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

func (s *LDAPService) Authenticate(username, password string) (*models.User, error) {
	// V3-C1: Reject empty passwords to prevent LDAP unauthenticated bind bypass (RFC 4513)
	if password == "" {
		return nil, fmt.Errorf("invalid credentials")
	}

	source, err := s.db.GetDefaultLDAPSource()
	if err != nil {
		// V3-M17: Return generic error, log details server-side
		slog.Error("LDAP source lookup failed", "error", err)
		return nil, fmt.Errorf("authentication failed")
	}

	var cfg LDAPConfig
	if err := json.Unmarshal([]byte(source.ConfigJSON), &cfg); err != nil {
		return nil, fmt.Errorf("invalid LDAP config: %w", err)
	}

	conn, err := s.connect(cfg)
	if err != nil {
		// V3-M17: Return generic error, log details server-side
		slog.Error("LDAP connection failed", "error", err)
		return nil, fmt.Errorf("authentication failed")
	}
	defer conn.Close()

	// Bind with service account
	if cfg.BindDN != "" {
		bindPass, err := s.enc.Decrypt(cfg.BindPasswordEncrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt bind password: %w", err)
		}
		if err := conn.Bind(cfg.BindDN, bindPass); err != nil {
			return nil, fmt.Errorf("LDAP bind: %w", err)
		}
	}

	// Search for user
	filter := strings.ReplaceAll(cfg.SearchFilter, "{{username}}", ldapv3.EscapeFilter(username))
	searchReq := ldapv3.NewSearchRequest(
		cfg.SearchBase,
		ldapv3.ScopeWholeSubtree,
		ldapv3.NeverDerefAliases,
		0, 0, false,
		filter,
		[]string{"dn", cfg.Attributes.Username, cfg.Attributes.DisplayName, cfg.Attributes.Email, "memberOf"},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		// V3-M17: Return generic error, log LDAP details server-side
		slog.Error("LDAP search failed", "error", err)
		return nil, fmt.Errorf("authentication failed")
	}

	if len(result.Entries) == 0 {
		slog.Warn("LDAP user not found", "username", username)
		return nil, fmt.Errorf("invalid credentials")
	}
	if len(result.Entries) > 1 {
		slog.Warn("LDAP multiple users matched", "username", username, "count", len(result.Entries))
		return nil, fmt.Errorf("invalid credentials")
	}

	entry := result.Entries[0]
	userDN := entry.DN

	// Bind as the user to verify password
	if err := conn.Bind(userDN, password); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Determine role from group membership
	role := "viewer"
	memberOf := entry.GetAttributeValues("memberOf")
	for _, group := range memberOf {
		if strings.EqualFold(group, cfg.AdminGroup) {
			role = "admin"
			break
		}
		if strings.EqualFold(group, cfg.UserGroup) {
			role = "user"
		}
	}

	displayName := entry.GetAttributeValue(cfg.Attributes.DisplayName)
	if displayName == "" {
		displayName = username
	}

	// Check if user exists locally
	existingUser, err := s.db.GetUserByExternalID(userDN)
	if err == nil {
		// V3-M3: Check if the user account is deactivated in Forgemill
		if !existingUser.IsActive {
			return nil, fmt.Errorf("account is disabled")
		}
		// V3-H9: Re-evaluate role from LDAP groups on each login
		existingUser.Role = role
		s.db.UpdateUserRole(existingUser.ID, role)
		s.db.UpdateUserLogin(existingUser.ID)
		return existingUser, nil
	}

	// Auto-create user
	newUser := &models.User{
		Username:     username,
		PasswordHash: "",
		DisplayName:  displayName,
		Role:         role,
		IsActive:     true,
		AuthSourceID: &source.ID,
		ExternalID:   userDN,
	}
	if err := s.db.CreateLDAPUser(newUser); err != nil {
		return nil, fmt.Errorf("create LDAP user: %w", err)
	}
	slog.Info("auto-created LDAP user", "username", username, "role", role)
	return newUser, nil
}

func (s *LDAPService) TestConnection(sourceID int64) error {
	source, err := s.db.GetAuthSource(sourceID)
	if err != nil {
		return fmt.Errorf("auth source not found: %w", err)
	}

	var cfg LDAPConfig
	if err := json.Unmarshal([]byte(source.ConfigJSON), &cfg); err != nil {
		return fmt.Errorf("invalid LDAP config: %w", err)
	}

	conn, err := s.connect(cfg)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer conn.Close()

	if cfg.BindDN != "" {
		bindPass, err := s.enc.Decrypt(cfg.BindPasswordEncrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt bind password: %w", err)
		}
		if err := conn.Bind(cfg.BindDN, bindPass); err != nil {
			return fmt.Errorf("bind failed: %w", err)
		}
	}

	return nil
}

// 5.10: LDAP connection with timeout to prevent hanging on unreachable servers
func (s *LDAPService) connect(cfg LDAPConfig) (*ldapv3.Conn, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Server, cfg.Port)
	timeout := 10 * time.Second

	if cfg.UseTLS {
		netConn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return nil, fmt.Errorf("ldap tls dial: %w", err)
		}
		tlsConn := tls.Client(netConn, &tls.Config{
			ServerName: cfg.Server,
		})
		if err := tlsConn.Handshake(); err != nil {
			netConn.Close()
			return nil, fmt.Errorf("ldap tls handshake: %w", err)
		}
		conn := ldapv3.NewConn(tlsConn, true)
		conn.Start()
		return conn, nil
	}

	// MED-07 fix: Log a warning when LDAP is used without TLS, since bind operations
	// transmit user credentials in cleartext over the network.
	slog.Warn("LDAP connection without TLS — credentials will be transmitted in cleartext",
		"server", cfg.Server, "port", cfg.Port)
	netConn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("ldap dial: %w", err)
	}
	conn := ldapv3.NewConn(netConn, false)
	conn.Start()
	return conn, nil
}

func (s *LDAPService) ListSources() ([]models.AuthSource, error) {
	return s.db.ListAuthSources()
}

func (s *LDAPService) CreateSource(a *models.AuthSource) error {
	return s.db.CreateAuthSource(a)
}

func (s *LDAPService) GetSource(id int64) (*models.AuthSource, error) {
	return s.db.GetAuthSource(id)
}

func (s *LDAPService) UpdateSource(a *models.AuthSource) error {
	return s.db.UpdateAuthSource(a)
}

func (s *LDAPService) DeleteSource(id int64) error {
	return s.db.DeleteAuthSource(id)
}
