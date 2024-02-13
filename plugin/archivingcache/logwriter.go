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

package archivingcache

import (
	"context"
	"time"

	contentwriterV1 "github.com/nlnwa/veidemann-api/go/contentwriter/v1"
	logV1 "github.com/nlnwa/veidemann-api/go/log/v1"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/pkg/serviceconnections"
	"github.com/nlnwa/veidemann-log-service/pkg/logservice"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type LogWriterClient struct {
	*serviceconnections.Connection
	*logservice.LogWriter
}

func NewLogWriterClient(opts ...serviceconnections.ConnectionOption) *LogWriterClient {
	return &LogWriterClient{
		Connection: serviceconnections.NewClientConn("LogWriterClient", opts...),
	}
}

func (l *LogWriterClient) Connect() error {
	if err := l.Connection.Connect(); err != nil {
		return err
	}
	l.LogWriter = &logservice.LogWriter{
		LogClient: logV1.NewLogClient(l.ClientConn),
	}
	return nil
}

// WriteCrawlLog stores a crawl log of a dns request/response.
func (l *LogWriterClient) WriteCrawlLog(record *contentwriterV1.WriteResponseMeta_RecordMeta, size int, requestedHost string, fetchStart time.Time, fetchDurationMs int64, ipAddress string, executionId string) error {
	crawlLog := &logV1.CrawlLog{
		ExecutionId:         executionId,
		RecordType:          "resource",
		RequestedUri:        "dns:" + requestedHost,
		DiscoveryPath:       "P",
		StatusCode:          1,
		TimeStamp:           timestamppb.New(time.Now().UTC()),
		FetchTimeStamp:      timestamppb.New(fetchStart),
		FetchTimeMs:         fetchDurationMs,
		IpAddress:           ipAddress,
		ContentType:         "text/dns",
		Size:                int64(size),
		WarcId:              record.GetWarcId(),
		BlockDigest:         record.GetBlockDigest(),
		PayloadDigest:       record.GetPayloadDigest(),
		CollectionFinalName: record.GetCollectionFinalName(),
		StorageRef:          record.GetStorageRef(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return l.LogWriter.WriteCrawlLogs(ctx, []*logV1.CrawlLog{crawlLog})
}
