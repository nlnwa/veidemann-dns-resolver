/*
 * Copyright 2021 National Library of Norway.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *       http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package connection

import (
	"context"
	"errors"
	"fmt"
	"google.golang.org/grpc"
	"strconv"
	"time"
)

// Connection configure a connection to a gRPC server. Connection are set by the Option
// values passed to New.
type Connection struct {
	Name           string
	Host           string
	Port           int
	ConnectTimeout time.Duration
	DialOptions    []grpc.DialOption
	Conn           *grpc.ClientConn
}

// Option configures how to dial to a service.
type Option interface {
	apply(*Connection)
}

// EmptyOption does not alter the configuration. It can be embedded in
// another structure to build custom connection Connection.
type EmptyOption struct{}

func (EmptyOption) apply(*Connection) {}

// funcOption wraps a function that modifies Connection into an
// implementation of the Option interface.
type funcOption struct {
	f func(*Connection)
}

func (fco *funcOption) apply(po *Connection) {
	fco.f(po)
}

func newFuncOption(f func(*Connection)) *funcOption {
	return &funcOption{
		f: f,
	}
}

func defaultOptions(serviceName string) *Connection {
	return &Connection{
		Name:           serviceName,
		Host:           "localhost",
		Port:           8052,
		ConnectTimeout: 10 * time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithInsecure(),
			grpc.WithBlock(),
		},
	}
}

func (opts *Connection) Addr() string {
	return opts.Host + ":" + strconv.Itoa(opts.Port)
}

func (opts *Connection) Dial() (*grpc.ClientConn, error) {
	if opts.Conn != nil {
		return opts.Conn, nil
	}
	dialCtx, dialCancel := context.WithTimeout(context.Background(), opts.ConnectTimeout)
	defer dialCancel()
	conn, err := grpc.DialContext(dialCtx, opts.Addr(), opts.DialOptions...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("failed to dial %s at %s within %s: %s", opts.Name, opts.Addr(),
				opts.ConnectTimeout, err)
		}
		return nil, err
	}
	opts.Conn = conn

	return conn, nil
}

func New(serviceName string, opts ...Option) *Connection {
	o := defaultOptions(serviceName)
	for _, opt := range opts {
		opt.apply(o)
	}
	return o
}

func WithHost(host string) Option {
	return newFuncOption(func(c *Connection) {
		c.Host = host
	})
}

func WithPort(port int) Option {
	return newFuncOption(func(c *Connection) {
		c.Port = port
	})
}

func WithDialOptions(dialOption ...grpc.DialOption) Option {
	return newFuncOption(func(c *Connection) {
		c.DialOptions = dialOption
	})
}

func WithConnectTimeout(connectTimeout time.Duration) Option {
	return newFuncOption(func(c *Connection) {
		c.ConnectTimeout = connectTimeout
	})
}
