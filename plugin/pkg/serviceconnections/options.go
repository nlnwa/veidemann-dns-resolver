/*
 * Copyright 2020 National Library of Norway.
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

package serviceconnections

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/coredns/coredns/plugin/pkg/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// connectionOptions configure a connection. connectionOptions are set by the ConnectionOption
// values passed to NewConnectionOptions.
type connectionOptions struct {
	serviceName    string
	host           string
	port           int
	connectTimeout time.Duration
	dialOptions    []grpc.DialOption
}

func (opts *connectionOptions) Addr() string {
	return opts.host + ":" + strconv.Itoa(opts.port)
}

// ConnectionOption configures how to connectService to a service.
type ConnectionOption interface {
	apply(*connectionOptions)
}

// EmptyConnectionOption does not alter the configuration. It can be embedded in
// another structure to build custom connection options.
type EmptyConnectionOption struct{}

func (EmptyConnectionOption) apply(*connectionOptions) {}

// funcConnectionOption wraps a function that modifies connectionOptions into an
// implementation of the ConnectionOption interface.
type funcConnectionOption struct {
	f func(*connectionOptions)
}

func (fco *funcConnectionOption) apply(po *connectionOptions) {
	fco.f(po)
}

func newFuncConnectionOption(f func(*connectionOptions)) *funcConnectionOption {
	return &funcConnectionOption{
		f: f,
	}
}

func defaultConnectionOptions(serviceName string) connectionOptions {
	return connectionOptions{
		serviceName:    serviceName,
		connectTimeout: 10 * time.Second,
	}
}

func (opts *connectionOptions) connectService() (*grpc.ClientConn, error) {
	dialOpts := append(opts.dialOptions,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	clientConn, err := grpc.NewClient(opts.Addr(), dialOpts...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("failed to connectService to %s at %s within %s: %s", opts.serviceName, opts.Addr(),
				opts.connectTimeout, err)
		}
		return nil, err
	}
	log.Infof("Connected to %s at %s", opts.serviceName, opts.Addr())
	return clientConn, nil
}

func WithHost(host string) ConnectionOption {
	return newFuncConnectionOption(func(c *connectionOptions) {
		c.host = host
	})
}

func WithPort(port int) ConnectionOption {
	return newFuncConnectionOption(func(c *connectionOptions) {
		c.port = port
	})
}

func WithDialOptions(dialOption ...grpc.DialOption) ConnectionOption {
	return newFuncConnectionOption(func(c *connectionOptions) {
		c.dialOptions = dialOption
	})
}
