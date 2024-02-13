package test

import (
	"io"
	"sync"

	logV1 "github.com/nlnwa/veidemann-api/go/log/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// LogServiceMock is used to implement ContentWriterServer.
type LogServiceMock struct {
	logV1.UnimplementedLogServer
	*grpc.Server

	m        sync.Mutex
	len      int
	CrawlLog *logV1.CrawlLog
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
		s.len++
		s.CrawlLog = req.GetCrawlLog()
		s.m.Unlock()
	}
	return nil
}

// Close implements ContentWriterServer
func (s *LogServiceMock) Close() {
	s.GracefulStop()
}

func (s *LogServiceMock) Reset() {
	s.m.Lock()
	s.len = 0
	s.CrawlLog = nil
	s.m.Unlock()
}

func (s *LogServiceMock) Len() int {
	return s.len
}

// NewContentWriterServerMock creates a new ContentWriterServerMock
func NewLogServiceMock() *LogServiceMock {
	logService := &LogServiceMock{
		Server: grpc.NewServer(),
	}
	logV1.RegisterLogServer(logService, logService)
	reflection.Register(logService.Server)

	return logService
}
