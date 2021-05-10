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
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

type Connection struct {
	*grpc.ClientConn
	opts *connectionOptions
}

func (c *Connection) Addr() string {
	return c.opts.Addr()
}

func (c *Connection) Connect() error {
	var err error
	c.ClientConn, err = c.opts.connectService()
	return err
}

func (c *Connection) Close() error {
	if c.ClientConn != nil {
		return c.ClientConn.Close()
	}
	return nil
}

func (c *Connection) Ready() bool {
	return c.ClientConn.GetState() == connectivity.Ready
}

func NewClientConn(serviceName string, opts ...ConnectionOption) *Connection {
	o := defaultConnectionOptions(serviceName)
	for _, opt := range opts {
		opt.apply(&o)
	}
	return &Connection{
		opts: &o,
	}
}
