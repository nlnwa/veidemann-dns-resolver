package archivingcache

import (
	"context"
	"crypto/sha1"
	"fmt"
	"github.com/golang/protobuf/ptypes"
	configV1 "github.com/nlnwa/veidemann-api/go/config/v1"
	contentwriterV1 "github.com/nlnwa/veidemann-api/go/contentwriter/v1"
	"github.com/nlnwa/veidemann-dns-resolver/plugin/pkg/connection"
	"io"
	"time"
)

// ContentWriterClient holds the connections for ContentWriter and Veidemann database
type ContentWriterClient struct {
	*connection.Connection
	client contentwriterV1.ContentWriterClient
}

// NewContentWriterClient creates a new ContentWriterClient object
func NewContentWriterClient(contentWriterHost string, contentWriterPort int) *ContentWriterClient {
	c := &ContentWriterClient{
		Connection: connection.New("contentWriter",
			connection.WithConnectTimeout(30*time.Second),
			connection.WithHost(contentWriterHost),
			connection.WithPort(contentWriterPort)),
	}

	return c
}

// connect establishes connections
func (c *ContentWriterClient) connect() error {
	if conn, err := c.Dial(); err != nil {
		return err
	} else {
		c.client = contentwriterV1.NewContentWriterClient(conn)
		return nil
	}
}

func (c *ContentWriterClient) disconnect() error {
	if c.ClientConn != nil {
		return c.Close()
	}
	return nil
}

// writeRecord writes a WARC record.
func (c *ContentWriterClient) writeRecord(payload []byte, fetchStart time.Time, requestedHost string, proxyAddr string, executionId string, collectionId string) ([]byte, *contentwriterV1.WriteReply, error) {
	ts, _ := ptypes.TimestampProto(fetchStart)

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
				FetchTimeStamp: ts,
				IpAddress:      proxyAddr,
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

	stream, err := c.client.Write(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open stream to content writer: %w", err)
	}

	if err := stream.Send(metaRequest); err != nil {
		if err == io.EOF {
			// get server side error
			_, err = stream.CloseAndRecv()
		}
		return nil, nil, fmt.Errorf("failed to send meta request to content writer: %w", err)
	}
	if err := stream.Send(payloadRequest); err != nil {
		if err == io.EOF {
			// get server side error
			_, err = stream.CloseAndRecv()
		}
		return nil, nil, fmt.Errorf("failed to send payload request to content writer: %w", err)
	}

	if reply, err := stream.CloseAndRecv(); err != nil && err != io.EOF {
		return nil, nil, fmt.Errorf("error closing stream to content writer: %w", err)
	} else {
		return payload, reply, nil
	}
}
