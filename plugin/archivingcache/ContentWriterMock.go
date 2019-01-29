package archivingcache

import (
	contentwriterV1 "github.com/nlnwa/veidemann-api-go/contentwriter/v1"
	"net"

	"context"
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"io"
	"strconv"
	"sync"
)

// Server is used to implement ContentWriterServer.
type Server struct {
	server  *grpc.Server
	Header  *contentwriterV1.Data
	Payload *contentwriterV1.Data
	Meta    *contentwriterV1.WriteRequestMeta
	Cancel  string
	m       sync.Mutex
}

// Write implements ContentWriterServer
func (s *Server) Write(stream contentwriterV1.ContentWriter_WriteServer) error {
	for {
		foo, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Server error: %v", err)
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
			if (x.Meta.CollectionRef != nil) {
				warcId += ":" + x.Meta.CollectionRef.Id
			}
			stream.SendAndClose(
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
func (s *Server) Flush(ctx context.Context, in *empty.Empty) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

// Delete implements ContentWriterServer
func (s *Server) Delete(ctx context.Context, in *empty.Empty) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

// Close implements ContentWriterServer
func (s *Server) Close() {
	s.server.Stop()
}

// NewCWServer creates a new ContentWriterServerMock
func NewCWServer(port int) *Server {
	lis, err := net.Listen("tcp", "localhost:"+strconv.Itoa(port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	cwServer := &Server{}
	cwServer.server = grpc.NewServer()
	contentwriterV1.RegisterContentWriterServer(cwServer.server, cwServer)
	// Register reflection service on gRPC Server.
	reflection.Register(cwServer.server)
	go func() {
		log.Debugf("Resolve listening on port: %d", port)
		cwServer.server.Serve(lis)
	}()

	return cwServer
}
