package ocm

import (
	"fmt"

	"github.com/golang-jwt/jwt/v4"
	sdkClient "github.com/openshift-online/ocm-sdk-go"
)

type Client struct {
	config       *Config
	logger       sdkClient.Logger
	connection   *sdkClient.Connection
	connUsername string

	Authorization Authorization
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

func (c *Client) newConnection() error {
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
		return fmt.Errorf("can't build OCM client connection: no client secret or token has been provided")
	}

	connection, err := builder.Build()
	if err != nil {
		return fmt.Errorf("can't build OCM client connection: %s", err.Error())
	}

	token, _, err := connection.Tokens()

	var connUsername string

	if err == nil {
		connUsername = getUsernameFromJWT(token)
	} else {
		connUsername = "anonymous"
	}

	c.connection = connection
	c.connUsername = connUsername

	return nil
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
