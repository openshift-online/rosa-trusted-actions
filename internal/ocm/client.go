package ocm

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	sdkClient "github.com/openshift-online/ocm-sdk-go"
)

// ocmConnection defines the OCM connection methods used directly by Client.
// It exists so tests can substitute a fake connection via Client.buildConn
// instead of talking to a real OCM backend.
// *sdkClient.Connection satisfies this interface.
type ocmConnection interface {
	Tokens(expiresIn ...time.Duration) (string, string, error)
	Close() error
}

type Client struct {
	config       *Config
	logger       sdkClient.Logger
	connection   ocmConnection
	connUsername string

	Authorization Authorization

	// buildConn overrides the default connection builder for testing.
	buildConn func(*Config) (ocmConnection, error)
}

type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	Tokens       []string
	Debug        bool
}

func NewClient(config Config) (*Client, error) {
	logger, err := sdkClient.NewGoLoggerBuilder().
		Debug(config.Debug).
		Build()
	if err != nil {
		return nil, fmt.Errorf("unable to build OCM logger: %s", err.Error())
	}

	client := &Client{
		config: &config,
		logger: logger,
	}
	err = client.newConnection()
	if err != nil {
		return nil, fmt.Errorf("unable to build OCM connection: %s", err.Error())
	}
	client.Authorization = &authorization{client: client}
	return client, nil
}

// Returns the username extracted from JWT token
func getUsernameFromJWT(token string) string {
	parser := new(jwt.Parser)
	jwtToken, _, err := parser.ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return "anonymous"
	}

	claims, ok := jwtToken.Claims.(jwt.MapClaims)
	if !ok {
		return "anonymous"
	}

	claim, ok := claims["username"]
	if !ok {
		return "anonymous"
	}

	username, ok := claim.(string)
	if !ok {
		return "anonymous"
	}

	return username
}

// sdkConn returns the underlying *sdkClient.Connection needed by authorization.
// c.connection is replaced in tests by a fake connection.
// Otherwise, c.connection is always a *sdkClient.Connection.
func (c *Client) sdkConn() (*sdkClient.Connection, error) {
	conn, ok := c.connection.(*sdkClient.Connection)
	if !ok {
		return nil, fmt.Errorf("ocm: client connection is not a *sdkClient.Connection")
	}
	return conn, nil
}

func (c *Client) newConnection() error {
	var conn ocmConnection
	var err error

	if c.buildConn != nil {
		conn, err = c.buildConn(c.config)
	} else {
		conn, err = c.buildDefaultConnection()
	}
	if err != nil {
		return err
	}

	token, _, err := conn.Tokens()

	var connUsername string

	if err == nil {
		connUsername = getUsernameFromJWT(token)
	} else {
		connUsername = "anonymous"
	}

	c.connection = conn
	c.connUsername = connUsername

	return nil
}

func (c *Client) buildDefaultConnection() (*sdkClient.Connection, error) {
	builder := sdkClient.NewConnectionBuilder().
		Logger(c.logger).
		URL(c.config.BaseURL).
		MetricsSubsystem("api_outbound")

	if c.config.ClientID != "" || c.config.ClientSecret != "" {
		builder = builder.Client(c.config.ClientID, c.config.ClientSecret)
	}
	if len(c.config.Tokens) > 0 {
		builder = builder.Tokens(c.config.Tokens...)
	} else if c.config.ClientSecret == "" {
		return nil, fmt.Errorf("can't build OCM client connection: no client secret or token has been provided")
	}

	connection, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("can't build OCM client connection: %s", err.Error())
	}

	return connection, nil
}

func (c *Client) Close() error {
	if c.connection != nil {
		return c.connection.Close()
	}

	return nil
}

type service struct {
	client *Client
}
