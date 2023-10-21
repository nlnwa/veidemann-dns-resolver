package test

import (
	"context"
	"fmt"
	contentwriterV1 "github.com/nlnwa/veidemann-api/go/contentwriter/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/emptypb"
	"io"
	"sync"
)

// ContentWriterMock is used to implement ContentWriterServer.
type ContentWriterMock struct {
	contentwriterV1.UnimplementedContentWriterServer
	*grpc.Server
	m sync.Mutex

	Cancel  string
	Header  *contentwriterV1.Data
	Payload *contentwriterV1.Data
	Meta    *contentwriterV1.WriteRequestMeta
}

// Write implements ContentWriterServer
func (s *ContentWriterMock) Write(stream contentwriterV1.ContentWriter_WriteServer) error {
	for {
		foo, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ContentWriterMock error: %w", err)
		}

		switch x := foo.Value.(type) {
		case *contentwriterV1.WriteRequest_ProtocolHeader:
			s.m.Lock()
			s.Header = x.ProtocolHeader
			s.m.Unlock()
		case *contentwriterV1.WriteRequest_Payload:
			s.m.Lock()
			s.Payload = x.Payload
			s.m.Unlock()
		case *contentwriterV1.WriteRequest_Meta:
			s.m.Lock()
			s.Meta = x.Meta
			warcId := "WarcId"
			if x.Meta.CollectionRef != nil {
				warcId += ":" + x.Meta.CollectionRef.Id
			}
			_ = stream.SendAndClose(
				&contentwriterV1.WriteReply{
					Meta: &contentwriterV1.WriteResponseMeta{
						RecordMeta: map[int32]*contentwriterV1.WriteResponseMeta_RecordMeta{
							0: {
								RecordNum:           0,
								BlockDigest:         "bd",
								PayloadDigest:       "pd",
								WarcId:              warcId,
								StorageRef:          "ref",
								CollectionFinalName: "cfn",
							},
						},
					},
				})
			s.m.Unlock()
		case *contentwriterV1.WriteRequest_Cancel:
			s.m.Lock()
			s.Cancel = x.Cancel
			s.m.Unlock()
		default:
			return fmt.Errorf("Unexpected type %T", x)
		}
	}
	return nil
}

// Flush implements ContentWriterServer
func (s *ContentWriterMock) Flush(ctx context.Context, in *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

// Delete implements ContentWriterServer
func (s *ContentWriterMock) Delete(ctx context.Context, in *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

// Close implements ContentWriterServer
func (s *ContentWriterMock) Close() {
	s.Server.GracefulStop()
}

func (s *ContentWriterMock) Reset() {
	s.Cancel = ""
	s.Header = nil
	s.Meta = nil
	s.Payload = nil
}

// NewContentWriterServerMock creates a new ContentWriterServerMock
func NewContentWriterServerMock() *ContentWriterMock {
	cwServer := &ContentWriterMock{
		Server: grpc.NewServer(),
	}
	contentwriterV1.RegisterContentWriterServer(cwServer.Server, cwServer)
	// Register reflection service on gRPC ContentWriterMock.
	reflection.Register(cwServer.Server)

	return cwServer
}
