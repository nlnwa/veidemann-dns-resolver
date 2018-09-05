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
)

// server is used to implement helloworld.GreeterServer.
type server struct {
	server  *grpc.Server
	Header  *vm.Data
	Payload *vm.Data
	Meta    *vm.WriteRequestMeta
	Cancel  string
}

// SayHello implements helloworld.GreeterServer
func (s *server) Write(stream vm.ContentWriter_WriteServer) error {
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
			s.Header = x.Header
			//fmt.Println("HEADER: ", x.Header)
		case *vm.WriteRequest_Payload:
			s.Payload = x.Payload
			//fmt.Println("PAYLOAD: ", x.Payload)
		case *vm.WriteRequest_Meta:
			s.Meta = x.Meta
			//fmt.Println("META: ", x.Meta)
			stream.SendAndClose(
				&vm.WriteReply{
					Meta: &vm.WriteResponseMeta{
						RecordMeta: map[int32]*vm.WriteResponseMeta_RecordMeta{
							0: &vm.WriteResponseMeta_RecordMeta{
								RecordNum:     0,
								BlockDigest:   "bd",
								PayloadDigest: "pd",
								WarcId:        "WarcId",
								StorageRef:    "ref",
							},
						},
					},
				})
		case *vm.WriteRequest_Cancel:
			s.Cancel = x.Cancel
			//fmt.Println("CANCEL: ", x.Cancel)
		default:
			return fmt.Errorf("Unexpected type %T", x)
		}
	}
	return nil
}

func (s *server) Flush(ctx context.Context, in *empty.Empty) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *server) Delete(ctx context.Context, in *empty.Empty) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *server) Close() {
	s.server.Stop()
}

func NewCWServer(port int) *server {
	lis, err := net.Listen("tcp", "localhost:"+strconv.Itoa(port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	cwServer := &server{}
	cwServer.server = grpc.NewServer()
	vm.RegisterContentWriterServer(cwServer.server, cwServer)
	// Register reflection service on gRPC server.
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
