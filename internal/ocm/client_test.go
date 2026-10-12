package ocm

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

var _ ocmConnection = &mockConnection{}

// mockConnection implements ocmConnection for testing.
type mockConnection struct {
	tokensFn func(...time.Duration) (string, string, error)
	closeErr error
}

func (m *mockConnection) Tokens(expiresIn ...time.Duration) (string, string, error) {
	if m.tokensFn != nil {
		return m.tokensFn(expiresIn...)
	}
	return "", "", nil
}

func (m *mockConnection) Close() error {
	return m.closeErr
}

// withMockConn returns a clientOption that injects a mock connection builder.
func withMockConn(conn ocmConnection, err error) clientOption {
	return func(c *Client) {
		c.buildConn = func(*Config) (ocmConnection, error) {
			return conn, err
		}
	}
}

func makeToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

func makeAccessToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	return makeToken(t, claims)
}

func TestGetUsernameFromJWT(t *testing.T) {
	tests := []struct {
		name  string
		token func(t *testing.T) string
		want  string
	}{
		{
			name:  "valid username",
			token: func(t *testing.T) string { return makeToken(t, jwt.MapClaims{"username": "alice"}) },
			want:  "alice",
		},
		{
			name:  "parse error",
			token: func(t *testing.T) string { return "not-a-jwt" },
			want:  "anonymous",
		},
		{
			name:  "missing claim",
			token: func(t *testing.T) string { return makeToken(t, jwt.MapClaims{"sub": "x"}) },
			want:  "anonymous",
		},
		{
			name:  "non-string claim",
			token: func(t *testing.T) string { return makeToken(t, jwt.MapClaims{"username": 123}) },
			want:  "anonymous",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getUsernameFromJWT(tt.token(t))

			if got != tt.want {
				t.Errorf("getUsernameFromJWT() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	accessToken := makeAccessToken(t, jwt.MapClaims{"username": "alice"})

	tests := []struct {
		name         string
		config       Config
		opts         []clientOption
		wantErr      string // substring; empty means success expected
		wantUsername string // checked only on success
	}{
		{
			name:   "mock connection with valid token",
			config: Config{BaseURL: "https://api.example.com"},
			opts: []clientOption{withMockConn(&mockConnection{
				tokensFn: func(...time.Duration) (string, string, error) {
					return accessToken, "", nil
				},
			}, nil)},
			wantUsername: "alice",
		},
		{
			name:   "tokens error falls back to anonymous",
			config: Config{BaseURL: "https://api.example.com"},
			opts: []clientOption{withMockConn(&mockConnection{
				tokensFn: func(...time.Duration) (string, string, error) {
					return "", "", errors.New("token expired")
				},
			}, nil)},
			wantUsername: "anonymous",
		},
		{
			name:    "builder returns error",
			config:  Config{BaseURL: "https://api.example.com"},
			opts:    []clientOption{withMockConn(nil, errors.New("connection refused"))},
			wantErr: "connection refused",
		},
		{
			name:    "no credentials",
			config:  Config{BaseURL: "https://api.example.com"},
			wantErr: "no client secret or token has been provided",
		},
		{
			name:    "client ID without secret or token",
			config:  Config{BaseURL: "https://api.example.com", ClientID: "some-id"},
			wantErr: "no client secret or token has been provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config, tt.opts...)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("NewClient() expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("NewClient() error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewClient() unexpected error: %v", err)
			}
			defer client.Close()

			if client.connUsername != tt.wantUsername {
				t.Errorf("connUsername = %q, want %q", client.connUsername, tt.wantUsername)
			}
			if client.Authorization == nil {
				t.Error("Authorization = nil, want non-nil")
			}
		})
	}
}

func TestClient_Close(t *testing.T) {
	tests := []struct {
		name    string
		client  *Client
		wantErr string
	}{
		{
			name:   "nil connection",
			client: &Client{},
		},
		{
			name:    "propagates error",
			client:  &Client{connection: &mockConnection{closeErr: errors.New("close failed")}},
			wantErr: "close failed",
		},
		{
			name:   "success",
			client: &Client{connection: &mockConnection{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.client.Close()

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("Close() expected error, got nil")
				}
				if err.Error() != tt.wantErr {
					t.Errorf("Close() error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Close() unexpected error: %v", err)
			}
		})
	}
}
