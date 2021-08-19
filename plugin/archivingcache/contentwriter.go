package archivingcache

import (
	"context"
	"crypto/sha1"
	"fmt"
	configV1 "github.com/nlnwa/veidemann-api/go/config/v1"
	contentwriterV1 "github.com/nlnwa/veidemann-api/go/contentwriter/v1"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/pkg/serviceconnections"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"time"
)

// ContentWriterClient holds the connections for ContentWriterClient and Veidemann database
type ContentWriterClient struct {
	*serviceconnections.Connection
	contentwriterV1.ContentWriterClient
}

// NewContentWriterClient creates a new ContentWriterClient object
func NewContentWriterClient(opts ...serviceconnections.ConnectionOption) *ContentWriterClient {
	return &ContentWriterClient{
		Connection: serviceconnections.NewClientConn("Content Writer", opts...),
	}
}

// Connect establishes connections
func (c *ContentWriterClient) Connect() error {
	if err := c.Connection.Connect(); err != nil {
		return err
	}
	c.ContentWriterClient = contentwriterV1.NewContentWriterClient(c.ClientConn)
	return nil
}

// WriteRecord writes a WARC record.
func (c *ContentWriterClient) WriteRecord(payload []byte, fetchStart time.Time, requestedHost string, ipAddress string, executionId string, collectionId string) (*contentwriterV1.WriteReply, error) {
	d := sha1.New()
	d.Write(payload)
	digest := fmt.Sprintf("sha1:%x", d.Sum(nil))

	metaRequest := &contentwriterV1.WriteRequest{
		Value: &contentwriterV1.WriteRequest_Meta{
			Meta: &contentwriterV1.WriteRequestMeta{
				RecordMeta: map[int32]*contentwriterV1.WriteRequestMeta_RecordMeta{
					0: {
						RecordNum:         0,
						Type:              contentwriterV1.RecordType_RESOURCE,
						RecordContentType: "text/dns",
						Size:              int64(len(payload)),
						BlockDigest:       digest,
						SubCollection:     configV1.Collection_DNS,
					},
				},
				TargetUri:      "dns:" + requestedHost,
				FetchTimeStamp: timestamppb.New(fetchStart),
				IpAddress:      ipAddress,
				ExecutionId:    executionId,
				CollectionRef: &configV1.ConfigRef{
					Kind: configV1.Kind_collection,
					Id:   collectionId,
				},
			},
		},
	}

	payloadRequest := &contentwriterV1.WriteRequest{
		Value: &contentwriterV1.WriteRequest_Payload{
			Payload: &contentwriterV1.Data{
				RecordNum: 0,
				Data:      payload,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := c.ContentWriterClient.Write(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream to content writer: %w", err)
	}

	if err := stream.Send(metaRequest); err != nil {
		if err == io.EOF {
			// get server side error
			_, err = stream.CloseAndRecv()
		}
		return nil, fmt.Errorf("failed to send meta request to content writer: %w", err)
	}

	if err := stream.Send(payloadRequest); err != nil {
		if err == io.EOF {
			// get server side error
			_, err = stream.CloseAndRecv()
		}
		return nil, fmt.Errorf("failed to send payload request to content writer: %w", err)
	}

	reply, err := stream.CloseAndRecv()
	if err == io.EOF {
		return reply, nil
	} else {
		return reply, err
	}
}
