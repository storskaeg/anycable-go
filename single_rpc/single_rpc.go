package single_rpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anycable/anycable-go/config"
	"github.com/anycable/anycable-go/metrics"
	"github.com/anycable/anycable-go/node"
	"github.com/apex/log"

	pb "github.com/anycable/anycable-go/protos"
	"google.golang.org/grpc"
)

const (
	retryInterval = 500
	invokeTimeout = 3000

	metricsRPCCalls    = "rpc_call_total"
	metricsRPCFailures = "rpc_error_total"
)

// Controller implements node.Controller interface for gRPC
type Controller struct {
	host    string
	client  pb.RPCClient
	metrics *metrics.Metrics
	log     *log.Entry
}

// NewController builds new Controller from config
func NewController(config *config.Config, metrics *metrics.Metrics) *Controller {

	metrics.RegisterCounter(metricsRPCCalls, "The total number of RPC calls")
	metrics.RegisterCounter(metricsRPCFailures, "The total number of failed RPC calls")

	return &Controller{log: log.WithField("context", "rpc"), metrics: metrics, host: config.RPCHost}
}

// Start initializes RPC connection pool
func (c *Controller) Start() error {
	conn, err := grpc.Dial(c.host, grpc.WithInsecure())

	if err == nil {
		c.log.Infof("RPC connection initialized: %s", c.host)
	}

	c.client = pb.NewRPCClient(conn)

	return err
}

// Shutdown closes connections
func (c *Controller) Shutdown() error {
	// TODO: no-op?
	return nil
}

// Authenticate performs Connect RPC call
func (c *Controller) Authenticate(path string, headers *map[string]string) (string, []string, error) {
	client := c.client

	op := func() (interface{}, error) {
		return client.Connect(context.Background(), &pb.ConnectionRequest{Path: path, Headers: *headers})
	}

	c.metrics.Counter(metricsRPCCalls).Inc()

	response, err := retry(op)

	if err != nil {
		c.metrics.Counter(metricsRPCFailures).Inc()

		return "", nil, err
	}

	if r, ok := response.(*pb.ConnectionResponse); ok {

		c.log.Debugf("Authenticate response: %v", r)

		if r.Status.String() == "SUCCESS" {
			return r.Identifiers, r.Transmissions, nil
		}

		return "", nil, fmt.Errorf("Application error: %s", r.ErrorMsg)
	}

	c.metrics.Counter(metricsRPCFailures).Inc()

	return "", nil, errors.New("Failed to deserialize connection response")
}

// Subscribe performs Command RPC call with "subscribe" command
func (c *Controller) Subscribe(sid string, id string, channel string) (*node.CommandResult, error) {
	client := c.client

	op := func() (interface{}, error) {
		return client.Command(context.Background(), &pb.CommandMessage{Command: "subscribe", Identifier: channel, ConnectionIdentifiers: id})
	}

	response, err := retry(op)

	return c.parseCommandResponse(response, err)
}

// Unsubscribe performs Command RPC call with "unsubscribe" command
func (c *Controller) Unsubscribe(sid string, id string, channel string) (*node.CommandResult, error) {
	client := c.client

	op := func() (interface{}, error) {
		return client.Command(context.Background(), &pb.CommandMessage{Command: "unsubscribe", Identifier: channel, ConnectionIdentifiers: id})
	}

	response, err := retry(op)

	return c.parseCommandResponse(response, err)
}

// Perform performs Command RPC call with "perform" command
func (c *Controller) Perform(sid string, id string, channel string, data string) (*node.CommandResult, error) {
	client := c.client

	op := func() (interface{}, error) {
		return client.Command(context.Background(), &pb.CommandMessage{Command: "message", Identifier: channel, ConnectionIdentifiers: id, Data: data})
	}

	response, err := retry(op)

	return c.parseCommandResponse(response, err)
}

// Disconnect performs disconnect RPC call
func (c *Controller) Disconnect(sid string, id string, subscriptions []string, path string, headers *map[string]string) error {
	client := c.client

	op := func() (interface{}, error) {
		return client.Disconnect(context.Background(), &pb.DisconnectRequest{Identifiers: id, Subscriptions: subscriptions, Path: path, Headers: *headers})
	}

	c.metrics.Counter(metricsRPCCalls).Inc()

	response, err := retry(op)

	if err != nil {
		c.metrics.Counter(metricsRPCFailures).Inc()
		return err
	}

	if r, ok := response.(*pb.DisconnectResponse); ok {
		c.log.Debugf("Disconnect response: %v", r)

		if r.Status.String() == "SUCCESS" {
			return nil
		}

		c.metrics.Counter(metricsRPCFailures).Inc()

		return fmt.Errorf("Application error: %s", r.ErrorMsg)
	}

	return errors.New("Failed to deserialize disconnect response")
}

func (c *Controller) parseCommandResponse(response interface{}, err error) (*node.CommandResult, error) {
	c.metrics.Counter(metricsRPCCalls).Inc()

	if err != nil {
		c.metrics.Counter(metricsRPCFailures).Inc()

		return nil, err
	}

	if r, ok := response.(*pb.CommandResponse); ok {
		c.log.Debugf("Command response: %v", r)

		res := &node.CommandResult{
			Disconnect:     r.Disconnect,
			StopAllStreams: r.StopStreams,
			Streams:        r.Streams,
			Transmissions:  r.Transmissions,
		}

		if r.Status.String() == "SUCCESS" {
			return res, nil
		}

		return res, fmt.Errorf("Application error: %s", r.ErrorMsg)
	}

	c.metrics.Counter(metricsRPCFailures).Inc()

	return nil, errors.New("Failed to deserialize command response")
}

func retry(callback func() (interface{}, error)) (res interface{}, err error) {
	attempts := invokeTimeout / retryInterval

	for i := 0; ; i++ {
		res, err = callback()

		if err == nil {
			return res, nil
		}

		if i >= (attempts - 1) {
			return nil, err
		}

		time.Sleep(retryInterval * time.Millisecond)
	}
}
