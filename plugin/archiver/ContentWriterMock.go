package archiver

import (
	vm "github.com/nlnwa/veidemann-dns-resolver/veidemann_api"
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
	Header  *vm.Data
	Payload *vm.Data
	Meta    *vm.WriteRequestMeta
	Cancel  string
	m       sync.Mutex
}

// Write implements ContentWriterServer
func (s *Server) Write(stream vm.ContentWriter_WriteServer) error {
	for {
		foo, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Server error: %v", err)
		}

		switch x := foo.Value.(type) {
		case *vm.WriteRequest_Header:
			s.m.Lock()
			s.Header = x.Header
			s.m.Unlock()
		case *vm.WriteRequest_Payload:
			s.m.Lock()
			s.Payload = x.Payload
			s.m.Unlock()
		case *vm.WriteRequest_Meta:
			s.m.Lock()
			s.Meta = x.Meta
			stream.SendAndClose(
				&vm.WriteReply{
					Meta: &vm.WriteResponseMeta{
						RecordMeta: map[int32]*vm.WriteResponseMeta_RecordMeta{
							0: {
								RecordNum:     0,
								BlockDigest:   "bd",
								PayloadDigest: "pd",
								WarcId:        "WarcId",
								StorageRef:    "ref",
							},
						},
					},
				})
			s.m.Unlock()
		case *vm.WriteRequest_Cancel:
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
	vm.RegisterContentWriterServer(cwServer.server, cwServer)
	// Register reflection service on gRPC Server.
	reflection.Register(cwServer.server)
	//if err := s.Serve(lis); err != nil {
	//	log.Fatalf("failed to serve: %v", err)
	//}
	go func() {
		log.Debugf("Resolve listening on port: %d", port)
		cwServer.server.Serve(lis)
	}()

	return cwServer
}
