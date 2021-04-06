package archivingcache

import (
	"fmt"
	logV1 "github.com/nlnwa/veidemann-api/go/log/v1"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"io"
	"sync"
)

// LogServiceMock is used to implement ContentWriterServer.
type LogServiceMock struct {
	logV1.UnimplementedLogServer
	*grpc.Server

	CrawlLogs []*logV1.CrawlLog
	m         sync.Mutex
}

func (s *LogServiceMock) WriteCrawlLog(stream logV1.Log_WriteCrawlLogServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		s.m.Lock()
		crawlLog := req.GetCrawlLog()
		s.CrawlLogs = append(s.CrawlLogs, crawlLog)
		s.m.Unlock()
	}
	return nil
}

// Close implements ContentWriterServer
func (s *LogServiceMock) Close() {
	s.GracefulStop()
}

// NewContentWriterServerMock creates a new ContentWriterServerMock
func NewLogServiceMock(port int) *LogServiceMock {
	addr := fmt.Sprintf("localhost:%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	logService := &LogServiceMock{
		Server: grpc.NewServer(),
	}
	logV1.RegisterLogServer(logService, logService)
	reflection.Register(logService.Server)
	go func() {
		log.Infof("LogServiceMock listening on port: %d", port)
		_ = logService.Serve(lis)
	}()

	return logService
}
